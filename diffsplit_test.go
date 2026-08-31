package main

import (
	"strings"
	"testing"
)

const sampleDiff = `diff --git a/foo.go b/foo.go
index 111..222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package foo
+// added
diff --git a/gone.go b/gone.go
deleted file mode 100644
index 333..000
--- a/gone.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package gone
-// bye
diff --git a/fresh.go b/fresh.go
new file mode 100644
index 000..444
--- /dev/null
+++ b/fresh.go
@@ -0,0 +1,1 @@
+package fresh
diff --git a/old.go b/moved.go
similarity index 90%
rename from old.go
rename to moved.go
index 555..666 100644
--- a/old.go
+++ b/moved.go
@@ -1,1 +1,1 @@
-// old
+// moved
`

func TestSplitUnifiedDiffLossless(t *testing.T) {
	segs := SplitUnifiedDiff(sampleDiff)
	var joined strings.Builder
	for _, s := range segs {
		joined.WriteString(s.Raw)
	}
	if joined.String() != sampleDiff {
		t.Fatal("concatenated segments do not reproduce the input")
	}
}

func TestSplitUnifiedDiffPaths(t *testing.T) {
	segs := SplitUnifiedDiff(sampleDiff)
	want := []string{"foo.go", "gone.go", "fresh.go", "moved.go"}
	if len(segs) != len(want) {
		t.Fatalf("got %d segments, want %d", len(segs), len(want))
	}
	for i, w := range want {
		if segs[i].Path != w {
			t.Errorf("segment %d path = %q, want %q", i, segs[i].Path, w)
		}
	}
}

func TestSplitUnifiedDiffNoGitHeaders(t *testing.T) {
	diff := "--- a/x.txt\n+++ b/x.txt\n@@ -1 +1 @@\n-a\n+b\n--- a/y.txt\n+++ b/y.txt\n@@ -1 +1 @@\n-c\n+d\n"
	segs := SplitUnifiedDiff(diff)
	if len(segs) != 2 || segs[0].Path != "x.txt" || segs[1].Path != "y.txt" {
		t.Fatalf("got %+v", segs)
	}
}

func TestDeriveNameStatus(t *testing.T) {
	got := deriveNameStatus(SplitUnifiedDiff(sampleDiff))
	want := "M\tfoo.go\t+1/-0\nD\tgone.go\t+0/-2\nA\tfresh.go\t+1/-0\nR\tmoved.go\t+1/-1\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSegmentStat(t *testing.T) {
	cases := []struct {
		raw     string
		status  string
		added   int
		deleted int
	}{
		{"diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-x\n+y\n", "M", 1, 1},
		{"diff --git a/n.go b/n.go\nnew file mode 100644\n+++ b/n.go\n@@ -0,0 +1,2 @@\n+a\n+b\n", "A", 2, 0},
		{"diff --git a/d.go b/d.go\ndeleted file mode 100644\n--- a/d.go\n@@ -1,2 +0,0 @@\n-a\n-b\n", "D", 0, 2},
		{"diff --git a/o.go b/r.go\nrename from o.go\nrename to r.go\n", "R", 0, 0},
		// Pins the +++/--- guard: diff-of-a-diff hunk bodies contain literal
		// file-header lines that must not count as churn.
		{"diff --git a/p.patch b/p.patch\n--- a/p.patch\n+++ b/p.patch\n@@ -1,4 +1,4 @@\n+++ b/inner.go\n--- a/inner.go\n+real\n-gone\n", "M", 1, 1},
		// Pins the inHunk gate: nothing before the first "@@ " counts.
		{"diff --git a/w.go b/w.go\n-preamble noise\n--- a/w.go\n+++ b/w.go\n@@ -1 +1 @@\n+y\n", "M", 1, 0},
	}
	for _, c := range cases {
		status, added, deleted := segmentStat(FileSegment{Path: "p", Raw: c.raw})
		if status != c.status || added != c.added || deleted != c.deleted {
			t.Errorf("segmentStat(%.30q) = %s +%d/-%d, want %s +%d/-%d",
				c.raw, status, added, deleted, c.status, c.added, c.deleted)
		}
	}
}
