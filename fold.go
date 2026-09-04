// Mechanical folding (spec S2 / handoff W1): deterministic, diffstat-only
// compression of the MODEL INPUT, run before the LLM call. Folded files stay
// in the changed-files list and are still grouped by the model — only their
// hunks are replaced with a summary line, and prompt rules direct them into
// deletion/mechanical cohorts. Grouping ground truth and repair are untouched.
package main

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// minFoldSize is deliberately conservative: a false fold hides real hunks from
// the model, and folding only pays off on genuine repetition.
const minFoldSize = 5

// maxSimilarSharePct caps how much of a PR's churn similar folding may hide in
// total (disjoint groups accumulate — two folds at 49% each would starve the
// model just as badly as one at 98%).
// A fold compresses a repetitive tail; when a group is most of the PR it IS
// the PR, and "verify the representative, skim the rest" becomes a claim about
// content nobody checked. Calibrated per PR against real ones (see
// TestFoldSimilarShareGuard): genuine mechanical fan-out hides 15.6%, 40.3%
// and 43.6% of churn across all its groups, while dbfle-bson-go#14 — where the
// group was the whole implementation — hides 58.8%. The real headroom is
// therefore 6 points, not 17. Deletion folds are deliberately exempt: their
// claim is honest about unread code, and a delete-only PR is legitimately ~all
// deletion (api.shoplineapp.com#16595 folds 96.6%).
const maxSimilarSharePct = 50

// maxSurvivorSharePct backstops the cap above on delete-heavy PRs, where it is
// provably unreachable: a similar fold's members are a subset of what deletion
// folding left behind, so once deletions hide half the diff no similar fold can
// exceed half of it, and the cap goes silent while a group hides ~everything
// still readable. This bounds hiding against what the model can actually READ.
// Deliberately loose: no PR in hand exercises it, and it is non-binding on every
// calibrated case (nothing deleted there, so the two bases are equal). Tighten
// only with evidence — splitting runs.jsonl's folded_lines_pct into deletion
// and similar shares would supply it.
const maxSurvivorSharePct = 90

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

// magnitude buckets churn by order of magnitude: 0–9 → 0, 10–99 → 1, 100–999 → 2…
// "similar change size" for the same-shape fold key.
func magnitude(n int) int {
	m := 0
	for n >= 10 {
		n /= 10
		m++
	}
	return m
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
	totalChurn := 0
	for _, s := range segs {
		if s.Path == "" {
			continue
		}
		seen[s.Path]++
		st, a, d := segmentStat(s)
		stats[s.Path] = stat{st, a, d}
		// Counts every segment, duplicate paths included, so the share guard
		// and runs.jsonl's folded_lines_pct measure against the same base.
		totalChurn += a + d
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
	deletionLines := 0
	for _, d := range delOrder {
		members := delGroups[d]
		if len(members) < minFoldSize {
			continue
		}
		f := Fold{Kind: "deletion", Members: members, Dir: d}
		for _, p := range members {
			f.Lines += stats[p].added + stats[p].deleted
		}
		deletionLines += f.Lines
		folds = append(folds, f)
	}

	// Similar folds key on the EXACT directory (handoff W1: 同目錄) —
	// deliberately finer than the deletion pass's topDir: repetition is a
	// per-package signal, deletion piles are a per-tree one. inFold assumes
	// every already-emitted fold is deletion-only (no Rep); revisit if a fold
	// kind with a Rep is ever emitted first.
	inFold := make(map[string]bool)
	for _, f := range folds {
		for _, p := range f.Members {
			inFold[p] = true
		}
	}
	simGroups := make(map[string][]string)
	var simOrder []string
	for _, s := range segs {
		p := s.Path
		if !foldable(p) || inFold[p] || stats[p].status == "D" {
			continue
		}
		st := stats[p]
		key := path.Dir(p) + "\x00" + path.Ext(p) + "\x00" + strconv.Itoa(magnitude(st.added+st.deleted))
		if len(simGroups[key]) == 0 {
			simOrder = append(simOrder, key)
		}
		simGroups[key] = append(simGroups[key], p)
	}
	similarLines := 0
	for _, k := range simOrder {
		group := simGroups[k]
		if len(group) < minFoldSize {
			continue
		}
		f := Fold{Kind: "similar"}
		best := -1
		for _, p := range group {
			if c := stats[p].added + stats[p].deleted; c > best {
				best, f.Rep = c, p
			}
		}
		for _, p := range group {
			if p == f.Rep {
				continue
			}
			f.Members = append(f.Members, p)
			f.Lines += stats[p].added + stats[p].deleted
		}
		if f.Lines == 0 {
			// A zero-churn group (pure renames) saves nothing and would point
			// the model at a representative with no hunks.
			continue
		}
		hidden := similarLines + f.Lines
		if hidden*100 > totalChurn*maxSimilarSharePct ||
			hidden*100 > (totalChurn-deletionLines)*maxSurvivorSharePct {
			// This group is the body of the PR (or of what survives deletion
			// folding), not a tail: fold nothing and let the model read every
			// file. Greedy first-fit in first-appearance order — a group that
			// doesn't fit is skipped and later, smaller groups may still fold.
			continue
		}
		similarLines += f.Lines
		folds = append(folds, f)
	}
	return folds
}

// foldLine renders one fold as a column-0 marker line inside the diff fence —
// like "### hunk", a column-0 line there is always tool-generated. All member
// paths are listed so the model can copy them verbatim into cohorts.
func foldLine(f Fold) string {
	switch f.Kind {
	case "deletion":
		return fmt.Sprintf("### DELETED: %d files under %s/ (-%d lines): %s\n",
			len(f.Members), f.Dir, f.Lines, strings.Join(f.Members, ", "))
	default: // similar
		return fmt.Sprintf("### SIMILAR (like %s): %s\n", f.Rep, strings.Join(f.Members, ", "))
	}
}
