package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// delSeg builds a deleted-file segment with 2 removed lines.
func delSeg(p string) FileSegment {
	return seg(p, "diff --git a/"+p+" b/"+p+"\ndeleted file mode 100644\n--- a/"+p+
		"\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-x\n-y\n")
}

// mechSeg builds a modified-file segment with `pairs` -/+ line pairs
// (churn = 2*pairs).
func mechSeg(p string, pairs int) FileSegment {
	return seg(p, "diff --git a/"+p+" b/"+p+"\n--- a/"+p+"\n+++ b/"+p+"\n@@ -1 +1 @@\n"+
		strings.Repeat("-x\n+y\n", pairs))
}

// renSeg builds a rename-only segment: no hunks, zero churn.
func renSeg(p string) FileSegment {
	return seg(p, "diff --git a/old_"+p+" b/"+p+"\nrename from old_"+p+"\nrename to "+p+"\n")
}

func fiveDels() []FileSegment {
	return []FileSegment{delSeg("old/a.go"), delSeg("old/b.go"), delSeg("old/c.go"),
		delSeg("old/d.go"), delSeg("old/e.go")}
}

func TestFoldDeletionsByDir(t *testing.T) {
	segs := append(testSegs(), fiveDels()...)
	segs = append(segs, delSeg("legacy/x.go")) // lone deletion in another dir: below threshold
	folds := foldSegments(segs)
	if len(folds) != 1 || folds[0].Kind != "deletion" {
		t.Fatalf("folds = %+v", folds)
	}
	f := folds[0]
	if len(f.Members) != 5 || f.Dir != "old" || f.Lines != 10 || f.Members[0] != "old/a.go" {
		t.Errorf("deletion fold = %+v", f)
	}
}

func TestFoldDeletionsBelowThreshold(t *testing.T) {
	segs := append(testSegs(),
		delSeg("old/a.go"), delSeg("old/b.go"), delSeg("old/c.go"), delSeg("old/d.go"))
	if folds := foldSegments(segs); len(folds) != 0 {
		t.Errorf("4 deletions must not fold: %+v", folds)
	}
}

func TestFoldSkipsDuplicatePaths(t *testing.T) {
	// A duplicated path is bare-only in the hunk index and must stay visible
	// to the model — with the dup excluded only 4 deletions remain: no fold.
	segs := []FileSegment{
		delSeg("old/a.go"), delSeg("old/a.go"),
		delSeg("old/b.go"), delSeg("old/c.go"), delSeg("old/d.go"), delSeg("old/e.go"),
	}
	if folds := foldSegments(segs); len(folds) != 0 {
		t.Errorf("duplicate path folded: %+v", folds)
	}
}

func TestTopDir(t *testing.T) {
	for p, want := range map[string]string{"a/b/c.go": "a", "root.go": "."} {
		if got := topDir(p); got != want {
			t.Errorf("topDir(%q) = %q, want %q", p, got, want)
		}
	}
}

// TestFoldOnlyDeletions pins the status filter: modified files in a deletion
// fold's directory stay out of it, whatever other folds they may form.
func TestFoldOnlyDeletions(t *testing.T) {
	segs := append(fiveDels(),
		mechSeg("old/p.go", 1), mechSeg("old/q.go", 1), mechSeg("old/r.go", 1),
		mechSeg("old/s.go", 1), mechSeg("old/t.go", 1))
	var del *Fold
	folds := foldSegments(segs)
	for i := range folds {
		if folds[i].Kind == "deletion" {
			del = &folds[i]
		}
	}
	if del == nil {
		t.Fatal("no deletion fold")
	}
	if len(del.Members) != 5 || del.Members[4] != "old/e.go" {
		t.Errorf("modified files swept into the deletion fold: %+v", del.Members)
	}
}

func TestFoldSimilarKeepsRepresentative(t *testing.T) {
	segs := []FileSegment{
		mechSeg("cfg/a.yaml", 1), mechSeg("cfg/b.yaml", 1), mechSeg("cfg/c.yaml", 1),
		mechSeg("cfg/d.yaml", 1), mechSeg("cfg/e.yaml", 4), // churn 8 — largest, same magnitude bucket
		mechSeg("other/x.go", 1),
		mechSeg("cfg/f.json", 1),     // same dir + magnitude, different ext: stays out
		mechSeg("cfg/sub/g.yaml", 1), // same ext + magnitude, different dir: stays out
	}
	folds := foldSegments(segs)
	if len(folds) != 1 || folds[0].Kind != "similar" {
		t.Fatalf("folds = %+v", folds)
	}
	f := folds[0]
	if f.Rep != "cfg/e.yaml" {
		t.Errorf("rep = %q, want cfg/e.yaml", f.Rep)
	}
	if len(f.Members) != 4 || f.Members[0] != "cfg/a.yaml" {
		t.Errorf("members must exclude the representative: %+v", f.Members)
	}
	if f.Lines != 8 { // 4 members × churn 2; the rep's 8 lines are still sent
		t.Errorf("Lines = %d, want 8", f.Lines)
	}
}

func TestFoldSimilarRepTieFirstWins(t *testing.T) {
	segs := []FileSegment{
		mechSeg("cfg/a.yaml", 2), mechSeg("cfg/b.yaml", 2), mechSeg("cfg/c.yaml", 1),
		mechSeg("cfg/d.yaml", 1), mechSeg("cfg/e.yaml", 1),
		// Padding: without it the group would be most of the PR and the share
		// guard would (correctly) refuse to fold it.
		mechSeg("other/pad.go", 20),
	}
	folds := foldSegments(segs)
	if len(folds) != 1 || folds[0].Rep != "cfg/a.yaml" {
		t.Errorf("tie must keep the first in diff order: %+v", folds)
	}
}

func TestFoldSimilarSkipsZeroChurn(t *testing.T) {
	segs := []FileSegment{renSeg("new/a.go"), renSeg("new/b.go"), renSeg("new/c.go"),
		renSeg("new/d.go"), renSeg("new/e.go")}
	if folds := foldSegments(segs); len(folds) != 0 {
		t.Errorf("zero-churn group folded: %+v", folds)
	}
}

func TestFoldSimilarSplitsByMagnitude(t *testing.T) {
	// Same dir+ext but churn 2 vs 200: different buckets, neither reaches 5.
	segs := []FileSegment{
		mechSeg("cfg/a.yaml", 1), mechSeg("cfg/b.yaml", 1), mechSeg("cfg/c.yaml", 1),
		mechSeg("cfg/d.yaml", 100), mechSeg("cfg/e.yaml", 100),
	}
	if folds := foldSegments(segs); len(folds) != 0 {
		t.Errorf("magnitude buckets must not merge: %+v", folds)
	}
}

func TestFoldDeletionTakesPrecedence(t *testing.T) {
	// 5 same-shape deleted files fold as deletion, not similar.
	folds := foldSegments(fiveDels())
	if len(folds) != 1 || folds[0].Kind != "deletion" {
		t.Errorf("folds = %+v", folds)
	}
}

func TestMagnitude(t *testing.T) {
	for n, want := range map[int]int{0: 0, 9: 0, 10: 1, 99: 1, 100: 2, 15000: 4} {
		if got := magnitude(n); got != want {
			t.Errorf("magnitude(%d) = %d, want %d", n, got, want)
		}
	}
}

// churnSeg builds a modified-file segment with exactly `churn` changed lines.
func churnSeg(p string, churn int) FileSegment {
	return seg(p, "diff --git a/"+p+" b/"+p+"\n--- a/"+p+"\n+++ b/"+p+"\n@@ -1 +1 @@\n"+
		strings.Repeat("+x\n", churn))
}

// TestFoldSimilarShareGuard pins the share guard against real PRs (diffstats
// fetched 2026-09-01 from starlinglabs). The first seven groups MUST keep
// folding — they are the mechanical fan-out the heuristic exists for. The last
// must NOT: its members are dbfle-bson-go#14's entire implementation, so
// "verify the representative and skim the rest" would be a claim about content
// nobody checked. Retuning the threshold has to keep every row's verdict.
func TestFoldSimilarShareGuard(t *testing.T) {
	cases := []struct {
		name     string
		churns   []int // one churn value per file in the group
		prChurn  int   // the whole PR's churn, group included
		wantFold bool
	}{
		{"obs#1328 shippingfeewhitelist", []int{15, 15, 20, 25, 30, 35, 40, 40, 55}, 3319, true},
		{"obs#1328 report", []int{11, 20, 30, 35, 45, 50, 68}, 3319, true},
		{"obs#1328 operationalbill", []int{10, 20, 35, 43, 61}, 3319, true},
		{"obs#1335 711cb small", []int{14, 20, 30, 40, 50, 55, 60, 70, 71, 85}, 5910, true},
		{"obs#1335 711cb large", []int{107, 150, 180, 200, 220, 240, 260, 300, 316, 655}, 5910, true},
		{"layout#523 src", []int{120, 130, 140, 148, 150, 379}, 3941, true},
		{"layout#523 tests", []int{150, 180, 200, 250, 252, 930}, 3941, true},
		{"dbfle#14 flebson core", []int{125, 128, 174, 269, 281, 800, 916}, 3022, false},
	}
	for _, c := range cases {
		var segs []FileSegment
		sum := 0
		for i, churn := range c.churns {
			segs = append(segs, churnSeg(fmt.Sprintf("pkg/f%d.go", i), churn))
			sum += churn
		}
		// One file outside the group carries the rest of the PR's churn, so the
		// guard sees the real denominator.
		if rest := c.prChurn - sum; rest > 0 {
			segs = append(segs, churnSeg("other/rest.txt", rest))
		}
		folded := false
		for _, f := range foldSegments(segs) {
			if f.Kind == "similar" {
				folded = true
			}
		}
		if folded != c.wantFold {
			t.Errorf("%s: hides %d of %d lines — folded=%v, want %v",
				c.name, sum-slices.Max(c.churns), c.prChurn, folded, c.wantFold)
		}
	}
}

// TestFoldSimilarShareGuardIsCumulative pins that disjoint similar groups share
// one budget: two groups at ~40% each may not both fold, or the model is left
// with two representatives and nothing else.
func TestFoldSimilarShareGuardIsCumulative(t *testing.T) {
	var segs []FileSegment
	for i := 0; i < 5; i++ { // group A: 5 × 80 = 400 churn, hides 320
		segs = append(segs, churnSeg(fmt.Sprintf("a/f%d.go", i), 80))
	}
	for i := 0; i < 5; i++ { // group B: same shape, another 400 / hides 320
		segs = append(segs, churnSeg(fmt.Sprintf("b/f%d.go", i), 80))
	}
	segs = append(segs, churnSeg("other/rest.txt", 200)) // PR total 1000
	folds := foldSegments(segs)
	// 320 alone is 32% and folds; 640 together is 64% and must not. The first
	// group in diff order takes the budget.
	if len(folds) != 1 || folds[0].Rep != "a/f0.go" {
		t.Errorf("cumulative budget not enforced (or wrong group won): %+v", folds)
	}
}
