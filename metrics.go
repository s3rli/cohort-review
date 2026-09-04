// Per-run JSONL metrics (handoff W4): one line per load attempt — successful,
// or failed — appended to runs.jsonl. misc_lines_pct (semantic misc churn
// share) is the tool's quality gauge; anchor stats feed the M3 anchor-presence
// decision. Always best-effort: a metrics failure never blocks the page.
//
// A run can append more than one line: each credential-pair attempt is its own
// line, and a render failure after a successful grouping appends both a full
// record and an error line. Full records always carry "cohorts"/"anchor";
// attempt-failure lines carry only ts/pr/error plus always-present zero pct
// fields — those zeros are the phantoms to filter out before averaging
// misc_lines_pct, or they deflate the gauge:
//
//	jq -s '[.[]|select(has("cohorts") and (has("error")|not))]|map(.misc_lines_pct)|add/length' runs.jsonl
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
	"unicode/utf8"
)

type anchorStats struct {
	DescChars   int    `json:"desc_chars"`
	DescBullets int    `json:"desc_bullets"`
	JiraKey     string `json:"jira_key,omitempty"`
}

type runRecord struct {
	TS             string         `json:"ts"`
	PR             string         `json:"pr"` // workspace/repo#id
	Lines          int            `json:"lines,omitempty"`
	Files          int            `json:"files,omitempty"`
	Cohorts        int            `json:"cohorts,omitempty"`
	Types          map[string]int `json:"types,omitempty"`
	MiscLinesPct   float64        `json:"misc_lines_pct"`
	FoldedLinesPct float64        `json:"folded_lines_pct"`
	Anchor         *anchorStats   `json:"anchor,omitempty"`
	Model          string         `json:"model,omitempty"`
	GroupSecs      float64        `json:"group_secs,omitempty"`
	Error          string         `json:"error,omitempty"`
}

// bulletLine spots "- x", "* x", "+ x", "1. x", "2) x" list lines — the
// cheapest proxy for "the author enumerated intents".
var bulletLine = regexp.MustCompile(`(?m)^[ \t]*(?:[-*+]|\d+[.)])[ \t]`)

// jiraKeyRe also matches key-shaped non-keys (HTTP-2, UTF-8, SHA-256,
// CVE-2024) — false positives INFLATE anchor presence, so sanity-check the
// distinct jira_key values before trusting the anchor-rate read.
var jiraKeyRe = regexp.MustCompile(`[A-Z]+-\d+`)

func prID(ref PRRef) string {
	return fmt.Sprintf("%s/%s#%d", ref.Workspace, ref.Repo, ref.Number)
}

func computeRecord(ref PRRef, model string, pr PullRequest, g Grouping, segs []FileSegment, groupSecs float64) runRecord {
	r := runRecord{
		TS:      time.Now().UTC().Format(time.RFC3339),
		PR:      prID(ref),
		Model:   model,
		Cohorts: len(g.Cohorts),
		Types:   map[string]int{},
		Anchor: &anchorStats{
			DescChars:   utf8.RuneCountInString(pr.Description),
			DescBullets: len(bulletLine.FindAllString(pr.Description, -1)),
			JiraKey:     jiraKeyRe.FindString(pr.Title),
		},
		GroupSecs: groupSecs,
	}
	if g.Degraded {
		r.Error = "grouping degraded to single cohort"
	}
	// pathChurn covers whole-path (bare) refs — including duplicate-path
	// segments, which are always bare-only; hunk refs count from the first
	// segment's hunks.
	pathChurn := map[string]int{}
	byPath := map[string]FileSegment{}
	for _, s := range segs {
		if s.Path == "" {
			continue
		}
		_, a, d := segmentStat(s)
		pathChurn[s.Path] += a + d
		r.Lines += a + d
		if _, ok := byPath[s.Path]; !ok {
			byPath[s.Path] = s
		}
	}
	r.Files = len(byPath)
	miscLines := 0
	for _, c := range g.Cohorts {
		k := c.Type
		if k == "" {
			k = "unclassified"
		}
		r.Types[k]++
		if c.Type != "misc" {
			continue
		}
		for _, f := range c.Files {
			if f.Hunks == nil {
				miscLines += pathChurn[f.Path]
			} else if s, ok := byPath[f.Path]; ok {
				miscLines += hunkChurn(s.Raw, f.Hunks)
			}
		}
	}
	if r.Lines > 0 {
		r.MiscLinesPct = 100 * float64(miscLines) / float64(r.Lines)
		r.FoldedLinesPct = 100 * float64(g.FoldedLines) / float64(r.Lines)
	}
	return r
}

// hunkChurn counts changed lines in the referenced hunks of one segment. Each
// hunk string starts at its "@@ " line, so segmentStat's counting rules —
// including the diff-of-a-diff +++/--- guard — apply unchanged.
func hunkChurn(raw string, refs []int) int {
	_, hunks := splitSegmentHunks(raw)
	n := 0
	for _, h := range refs {
		if h >= 1 && h <= len(hunks) {
			_, a, d := segmentStat(FileSegment{Raw: hunks[h-1]})
			n += a + d
		}
	}
	return n
}

func appendJSONL(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}
