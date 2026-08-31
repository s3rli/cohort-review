package main

import (
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
