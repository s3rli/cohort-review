// Mechanical folding (spec S2 / handoff W1): deterministic, diffstat-only
// compression of the MODEL INPUT, run before the LLM call. Folded files stay
// in the changed-files list and are still grouped by the model — only their
// hunks are replaced with a summary line, and prompt rules direct them into
// deletion/mechanical cohorts. Grouping ground truth and repair are untouched.
package main

import "strings"

// minFoldSize is deliberately conservative: a false fold hides real hunks from
// the model, and folding only pays off on genuine repetition.
const minFoldSize = 5

// A Fold is one planned summary line: files whose hunks the model won't see.
type Fold struct {
	Kind    string   // "deletion" | "similar"
	Members []string // paths whose hunks are replaced, diff order
	Rep     string   // similar folds: representative whose hunks ARE still sent
	Dir     string   // deletion folds: shared top-level directory label
	Lines   int      // churn folded out of the prompt (members only)
}

func topDir(p string) string {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return "."
}

// foldSegments plans prompt-side folding. Preamble (empty Path) and duplicate
// paths are never folded: duplicates are bare-only in the hunk index and must
// stay visible to the model.
func foldSegments(segs []FileSegment) []Fold {
	type stat struct {
		status         string
		added, deleted int
	}
	stats := make(map[string]stat, len(segs))
	seen := make(map[string]int, len(segs))
	for _, s := range segs {
		if s.Path == "" {
			continue
		}
		seen[s.Path]++
		st, a, d := segmentStat(s)
		stats[s.Path] = stat{st, a, d}
	}
	foldable := func(p string) bool { return p != "" && seen[p] == 1 }

	// Deletion folds: status-D files grouped by top-level directory.
	delGroups := make(map[string][]string)
	var delOrder []string
	for _, s := range segs {
		if !foldable(s.Path) || stats[s.Path].status != "D" {
			continue
		}
		d := topDir(s.Path)
		if len(delGroups[d]) == 0 {
			delOrder = append(delOrder, d)
		}
		delGroups[d] = append(delGroups[d], s.Path)
	}
	var folds []Fold
	for _, d := range delOrder {
		members := delGroups[d]
		if len(members) < minFoldSize {
			continue
		}
		f := Fold{Kind: "deletion", Members: members, Dir: d}
		for _, p := range members {
			f.Lines += stats[p].added + stats[p].deleted
		}
		folds = append(folds, f)
	}
	return folds
}
