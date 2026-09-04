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
		// Inside a hunk, "+++"/"---" lines are ordinary changes whose content
		// starts with "++"/"--" — not file headers, which cannot appear after
		// the first "@@ ". Counting them as churn is what the fold share guard
		// and metrics depend on.
		{"diff --git a/p.patch b/p.patch\n--- a/p.patch\n+++ b/p.patch\n@@ -1,4 +1,4 @@\n+++ b/inner.go\n--- a/inner.go\n+real\n-gone\n", "M", 2, 2},
		// The reachable real-world shapes: prefix increment/decrement in
		// C-like code, and a removed YAML/front-matter "---" separator.
		{"diff --git a/x.js b/x.js\n--- a/x.js\n+++ b/x.js\n@@ -1,2 +1,2 @@\n+++counter;\n---counter;\n", "M", 1, 1},
		{"diff --git a/c.yml b/c.yml\n--- a/c.yml\n+++ b/c.yml\n@@ -1 +1 @@\n----\n+key: v\n", "M", 1, 1},
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

// TestRecoverSegmentPaths pins that files with no ---/+++ headers — binary
// changes, pure renames, mode-only changes — are still named. Without this a
// PR made only of such files looks like it has no diff at all.
func TestRecoverSegmentPaths(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"diff --git a/img/run.png b/img/run.png\ndeleted file mode 100644\nBinary files a/img/run.png and /dev/null differ\n", "img/run.png"},
		{"diff --git a/old.go b/new.go\nsimilarity index 100%\nrename from old.go\nrename to new.go\n", "new.go"},
		{"diff --git a/s.sh b/s.sh\nold mode 100644\nnew mode 100755\n", "s.sh"},
		{"diff --git a/has space.txt b/has space.txt\nBinary files differ\n", "has space.txt"},
		// Git C-quotes unusual paths; those stay unrecovered rather than mangled.
		{"diff --git \"a/od\\303\\251.txt\" \"b/od\\303\\251.txt\"\nBinary files differ\n", ""},
		// Preamble content is not a file and must stay empty.
		{"warning: something\n", ""},
	}
	for _, c := range cases {
		got := recoverSegmentPaths([]FileSegment{{Raw: c.raw}})[0].Path
		if got != c.want {
			t.Errorf("recoverSegmentPaths(%.40q) = %q, want %q", c.raw, got, c.want)
		}
	}
	// An already-resolved path is never overwritten.
	segs := recoverSegmentPaths([]FileSegment{{Path: "real.go", Raw: "diff --git a/other.go b/other.go\n"}})
	if segs[0].Path != "real.go" {
		t.Errorf("existing path overwritten: %q", segs[0].Path)
	}
}
