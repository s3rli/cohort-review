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
