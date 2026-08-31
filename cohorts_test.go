package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func seg(path, body string) FileSegment {
	return FileSegment{Path: path, Raw: body}
}

func testSegs() []FileSegment {
	return []FileSegment{
		seg("core/auth.go", "diff --git a/core/auth.go b/core/auth.go\n@@ -1 +1 @@\n-x\n+y\n"),
		seg("core/session.go", "diff --git a/core/session.go b/core/session.go\n@@ -1 +1 @@\n-x\n+y\n"),
		seg("docs/README.md", "diff --git a/docs/README.md b/docs/README.md\n@@ -1 +1 @@\n-x\n+y\n"),
	}
}

// splitSegs has multi-hunk files plus a hunkless binary — the fixture for
// hunk-level grouping tests. auth: 3 hunks, util: 2 hunks, img.png: 0.
func splitSegs() []FileSegment {
	return []FileSegment{
		seg("core/auth.go", "diff --git a/core/auth.go b/core/auth.go\n@@ -1 +1 @@\n-a1\n+a1x\n@@ -10 +10 @@\n-a2\n+a2x\n@@ -20 +20 @@\n-a3\n+a3x\n"),
		seg("core/util.go", "diff --git a/core/util.go b/core/util.go\n@@ -1 +1 @@\n-u1\n+u1x\n@@ -9 +9 @@\n-u2\n+u2x\n"),
		seg("img.png", "diff --git a/img.png b/img.png\nBinary files differ\n"),
	}
}

// --- hunk index / ref resolution ---

func TestIndexSegments(t *testing.T) {
	idx := indexSegments(splitSegs())
	wantPaths := []string{"core/auth.go", "core/util.go", "img.png"}
	if len(idx.paths) != len(wantPaths) {
		t.Fatalf("paths = %v", idx.paths)
	}
	for i, p := range wantPaths {
		if idx.paths[i] != p {
			t.Errorf("paths[%d] = %q, want %q", i, idx.paths[i], p)
		}
	}
	for p, want := range map[string]int{"core/auth.go": 3, "core/util.go": 2, "img.png": 0} {
		if idx.hunks[p] != want {
			t.Errorf("hunks[%s] = %d, want %d", p, idx.hunks[p], want)
		}
	}

	// A path appearing in two segments is bare-only and listed once.
	dup := []FileSegment{
		seg("x.go", "diff --git a/x.go b/x.go\n@@ -1 +1 @@\n-a\n+b\n"),
		seg("x.go", "diff --git a/x.go b/x.go\n@@ -5 +5 @@\n-c\n+d\n"),
	}
	idx = indexSegments(dup)
	if len(idx.paths) != 1 || idx.hunks["x.go"] != 0 {
		t.Errorf("duplicate path: paths=%v hunks=%v", idx.paths, idx.hunks)
	}
}

func TestResolveRef(t *testing.T) {
	idx := indexSegments(splitSegs())
	valid := map[string]bool{}
	for _, p := range idx.paths {
		valid[p] = true
	}
	cases := []struct {
		in   string
		path string
		hunk int
		ok   bool
	}{
		{"core/auth.go", "core/auth.go", 0, true},
		{"core/auth.go#2", "core/auth.go", 2, true},
		{"core/auth.go#0", "", 0, false},
		{"core/auth.go#4", "", 0, false},
		{"img.png#1", "", 0, false},
		{"core/auth.go#x", "", 0, false},
		{"made/up.go", "", 0, false},
		{"`core/util.go#2`", "core/util.go", 2, true},
		{"b/core/util.go#1", "core/util.go", 1, true},
		{"core/util.go#2\t+9/-2", "core/util.go", 2, true},
	}
	for _, c := range cases {
		p, n, ok := resolveRef(c.in, idx, valid)
		if p != c.path || n != c.hunk || ok != c.ok {
			t.Errorf("resolveRef(%q) = (%q, %d, %v), want (%q, %d, %v)", c.in, p, n, ok, c.path, c.hunk, c.ok)
		}
	}

	// A literal '#' filename wins over hunk addressing.
	lit := indexSegments([]FileSegment{
		seg("notes", "diff --git a/notes b/notes\n@@ -1 +1 @@\n-a\n+b\n@@ -5 +5 @@\n-c\n+d\n"),
		seg("notes#2", "diff --git a/notes#2 b/notes#2\n@@ -1 +1 @@\n-e\n+f\n"),
	})
	lv := map[string]bool{"notes": true, "notes#2": true}
	if p, n, ok := resolveRef("notes#2", lit, lv); !ok || p != "notes#2" || n != 0 {
		t.Errorf("literal # filename: got (%q, %d, %v)", p, n, ok)
	}
}

func TestAnnotateSegmentHunks(t *testing.T) {
	for _, s := range splitSegs() {
		ann := annotateSegmentHunks(s.Path, s.Raw)
		var stripped strings.Builder
		for _, line := range strings.SplitAfter(ann, "\n") {
			if strings.HasPrefix(line, "### hunk ") {
				continue
			}
			stripped.WriteString(line)
		}
		if stripped.String() != s.Raw {
			t.Errorf("%s: stripping markers does not reproduce raw", s.Path)
		}
	}

	ann := annotateSegmentHunks("core/auth.go", splitSegs()[0].Raw)
	for n := 1; n <= 3; n++ {
		marker := fmt.Sprintf("### hunk core/auth.go#%d\n@@ ", n)
		if !strings.Contains(ann, marker) {
			t.Errorf("marker %d not immediately before its hunk", n)
		}
	}

	binary := splitSegs()[2]
	if annotateSegmentHunks(binary.Path, binary.Raw) != binary.Raw {
		t.Error("hunkless segment must pass through unchanged")
	}
}

// --- budget assembly ---

// annLen is a segment's prompt footprint: its annotated length.
func annLen(s FileSegment) int {
	return len(annotateSegmentHunks(s.Path, s.Raw))
}

func TestBuildPromptInputUnderBudget(t *testing.T) {
	in := buildPromptInput(testSegs(), defaultBudget)
	if len(in.Omitted) != 0 {
		t.Errorf("unexpected omissions: %v", in.Omitted)
	}
	for _, p := range []string{"core/auth.go", "core/session.go", "docs/README.md"} {
		if !strings.Contains(in.Hunks, p) {
			t.Errorf("hunks missing %s", p)
		}
		if !strings.Contains(in.Hunks, "### hunk "+p+"#1") {
			t.Errorf("hunk marker missing for %s", p)
		}
		if !strings.Contains(in.NameStatus, "M\t"+p) {
			t.Errorf("name-status missing %s", p)
		}
	}
}

func TestBuildPromptInputGreedySkip(t *testing.T) {
	// One huge single hunk: nothing to truncate at a hunk boundary, so the
	// segment must still be skipped whole.
	giant := seg("vendor/lock.json", "diff --git a/vendor/lock.json b/vendor/lock.json\n@@ -1,200 +1,200 @@\n"+strings.Repeat("+x\n", 200))
	segs := append([]FileSegment{giant}, testSegs()...)
	budget := annLen(testSegs()[0]) + annLen(testSegs()[1]) + annLen(testSegs()[2])
	in := buildPromptInput(segs, budget)
	// The giant first segment is skipped whole; every later small file still fits.
	if len(in.Omitted) != 1 || in.Omitted[0] != "vendor/lock.json" {
		t.Fatalf("omitted = %v, want [vendor/lock.json]", in.Omitted)
	}
	if strings.Contains(in.Hunks, "vendor/lock.json") {
		t.Error("giant segment leaked into hunks")
	}
	if !strings.Contains(in.Hunks, "core/session.go") {
		t.Error("later small segment evicted by giant one")
	}
	// Name-status stays complete regardless of budget.
	if !strings.Contains(in.NameStatus, "vendor/lock.json") {
		t.Error("name-status must always list every file")
	}
}

func TestSplitSegmentHunks(t *testing.T) {
	for _, s := range SplitUnifiedDiff(sampleDiff) {
		header, hunks := splitSegmentHunks(s.Raw)
		if header+strings.Join(hunks, "") != s.Raw {
			t.Errorf("%s: header+hunks does not reproduce raw", s.Path)
		}
		for i, h := range hunks {
			if !strings.HasPrefix(h, "@@ ") {
				t.Errorf("%s: hunk %d does not start with @@: %q", s.Path, i, h)
			}
		}
	}

	multi := "diff --git a/m.go b/m.go\n--- a/m.go\n+++ b/m.go\n" +
		"@@ -1 +1 @@\n-a\n+b\n@@ -10 +10 @@\n-c\n+d\n@@ -20 +20 @@\n-e\n+f\n"
	header, hunks := splitSegmentHunks(multi)
	if len(hunks) != 3 || header+strings.Join(hunks, "") != multi {
		t.Errorf("multi-hunk split: header %q, %d hunks", header, len(hunks))
	}

	binary := "diff --git a/img.png b/img.png\nBinary files differ\n"
	header, hunks = splitSegmentHunks(binary)
	if header != binary || len(hunks) != 0 {
		t.Errorf("hunkless segment: header %q, %d hunks", header, len(hunks))
	}
}

func TestBuildPromptInputTruncatesLeadingHunks(t *testing.T) {
	header := "diff --git a/big.go b/big.go\n"
	hunk1 := "@@ -1 +1 @@\n-first_hunk_old\n+first_hunk_new\n"
	hunk2 := "@@ -10,200 +10,200 @@\n" + strings.Repeat("+pad\n", 200)
	giant := seg("big.go", header+hunk1+hunk2)
	segs := append([]FileSegment{giant}, testSegs()...)
	// Budget makes the per-file cap exactly header+hunk1: the giant is
	// truncated after its first hunk instead of being omitted whole.
	budget := (len(header) + len(hunk1)) * 8
	in := buildPromptInput(segs, budget)
	if len(in.Truncated) != 1 || in.Truncated[0] != "big.go" {
		t.Fatalf("truncated = %v, want [big.go]", in.Truncated)
	}
	if len(in.Omitted) != 0 {
		t.Errorf("unexpected omissions: %v", in.Omitted)
	}
	if !strings.Contains(in.Hunks, "first_hunk_new") {
		t.Error("leading hunk missing from prompt hunks")
	}
	if strings.Contains(in.Hunks, "+pad") {
		t.Error("trailing hunk leaked past the cap (cut should be at a hunk boundary)")
	}
	if !strings.Contains(in.Hunks, "core/session.go") {
		t.Error("later small segment evicted by truncated giant")
	}
	// Only the surviving leading hunks are annotated.
	if !strings.Contains(in.Hunks, "### hunk big.go#1") {
		t.Error("truncated file's leading hunk not annotated")
	}
	if strings.Contains(in.Hunks, "big.go#2") {
		t.Error("unseen hunk of truncated file must not be annotated")
	}
}

func TestBuildPromptInputTruncationFallsBackToOmitted(t *testing.T) {
	header := "diff --git a/big.go b/big.go\n"
	hunk1 := "@@ -1,50 +1,50 @@\n" + strings.Repeat("+first_hunk\n", 50)
	giant := seg("big.go", header+hunk1+"@@ -100,200 +100,200 @@\n"+strings.Repeat("+pad\n", 200))
	segs := append([]FileSegment{giant}, testSegs()...)
	// The cap (budget/8) is far below header+first hunk: truncation is
	// impossible and the file degrades to omission, as before truncation
	// existed. The small files still fit whole.
	budget := 600
	in := buildPromptInput(segs, budget)
	if len(in.Omitted) != 1 || in.Omitted[0] != "big.go" {
		t.Fatalf("omitted = %v, want [big.go]", in.Omitted)
	}
	if len(in.Truncated) != 0 {
		t.Errorf("unexpected truncations: %v", in.Truncated)
	}
	if strings.Contains(in.Hunks, "first_hunk") {
		t.Error("omitted segment leaked into hunks")
	}
}

func TestSplitRulesInPrompt(t *testing.T) {
	p := buildCohortPrompt(promptInput{NameStatus: "M\ta.go\t+1/-0\n"}, "nonce")
	for _, frag := range []string{"path#N", "### hunk", "EXACTLY ONE cohort", "omitted or truncated",
		"same cohort as its system under test", "mechanical test churn"} {
		if !strings.Contains(p, frag) {
			t.Errorf("prompt missing rule fragment %q", frag)
		}
	}
	// Tests are the change's specification, not mechanical noise: the
	// mechanical-changes list must not sweep them into a trailing cohort.
	if strings.Contains(p, "mechanical changes (tests") {
		t.Error("tests still listed as mechanical changes")
	}
}

func TestTruncatedNoteInPrompt(t *testing.T) {
	in := promptInput{NameStatus: "M\ta.go\t+1/-0\n", Truncated: []string{"big1.go", "big2.go"}}
	p := buildCohortPrompt(in, "nonce")
	if !strings.Contains(p, "TRUNCATED") || !strings.Contains(p, "big1.go, big2.go") {
		t.Error("truncated note missing or incomplete")
	}
}

func TestOmittedNoteInPrompt(t *testing.T) {
	in := promptInput{NameStatus: "M\ta.go\n", Hunks: "", Omitted: []string{"big1.go", "big2.go"}}
	p := buildCohortPrompt(in, "nonce")
	if !strings.Contains(p, "MUST still be grouped") || !strings.Contains(p, "big1.go, big2.go") {
		t.Error("omitted note missing or incomplete")
	}
	if !strings.Contains(p, "UNTRUSTED-INPUT-nonce BEGIN changed files") {
		t.Error("changed-files fence missing")
	}
}

func TestPRMetadataInPrompt(t *testing.T) {
	in := promptInput{Title: "Add rate limiting", Description: "Protects the API.", NameStatus: "M\ta.go\t+1/-0\n"}
	p := buildCohortPrompt(in, "nonce")
	if !strings.Contains(p, "UNTRUSTED-INPUT-nonce BEGIN pr title and description") {
		t.Error("pr metadata fence missing")
	}
	if !strings.Contains(p, "Add rate limiting") || !strings.Contains(p, "Protects the API.") {
		t.Error("title or description text missing")
	}
	if !strings.Contains(p, "reading order") {
		t.Error("within-cohort ordering rule missing")
	}
	empty := buildCohortPrompt(promptInput{NameStatus: "M\ta.go\t+1/-0\n"}, "nonce")
	if strings.Contains(empty, "## PR title and description") {
		t.Error("metadata section present despite empty title and description")
	}
}

// --- parse / repair / fallback ---

func canned(outputs ...string) groupFunc {
	i := 0
	return func(ctx context.Context, prompt string) (string, error) {
		if i >= len(outputs) {
			return "", errors.New("no more outputs")
		}
		out := outputs[i]
		i++
		return out, nil
	}
}

const goodJSON = `{"walkthrough": "Adds auth.", "cohorts": [
	{"name": "Auth", "summary": "core auth", "claim": "Does auth validate sessions?", "type": "claim", "files": ["core/auth.go", "core/session.go"]},
	{"name": "Docs", "summary": "docs", "claim": "Do the docs match the new flow?", "type": "claim", "files": ["docs/README.md"]}]}`

func TestGroupCohortsCleanJSON(t *testing.T) {
	g := groupCohorts(context.Background(), canned(goodJSON), PullRequest{}, testSegs(), defaultBudget)
	if g.Walkthrough != "Adds auth." || len(g.Cohorts) != 2 {
		t.Fatalf("got %+v", g)
	}
}

func TestGroupCohortsFencedAndProseWrapped(t *testing.T) {
	for _, out := range []string{
		"```json\n" + goodJSON + "\n```",
		"Here is the grouping:\n" + goodJSON + "\nHope this helps!",
	} {
		g := groupCohorts(context.Background(), canned(out), PullRequest{}, testSegs(), defaultBudget)
		if len(g.Cohorts) != 2 {
			t.Errorf("output %q: got %d cohorts", out[:20], len(g.Cohorts))
		}
	}
}

func TestRepairHallucinatedDroppedMissedToOther(t *testing.T) {
	out := `{"walkthrough": "w", "cohorts": [
		{"name": "Auth", "summary": "s", "files": ["core/auth.go", "made/up.go"]}]}`
	g := groupCohorts(context.Background(), canned(out), PullRequest{}, testSegs(), defaultBudget)
	if len(g.Cohorts) != 2 {
		t.Fatalf("got %+v", g.Cohorts)
	}
	if g.Cohorts[0].Files[0].Path != "core/auth.go" || len(g.Cohorts[0].Files) != 1 {
		t.Errorf("hallucinated path not dropped: %v", g.Cohorts[0].Files)
	}
	last := g.Cohorts[len(g.Cohorts)-1]
	if last.Name != "Other" || len(last.Files) != 2 {
		t.Errorf("missed files not gathered into Other: %+v", last)
	}
}

func TestRepairDuplicateKeptFirst(t *testing.T) {
	out := `{"cohorts": [
		{"name": "A", "summary": "s", "files": ["core/auth.go"]},
		{"name": "B", "summary": "s", "files": ["core/auth.go", "core/session.go", "docs/README.md"]}]}`
	g := groupCohorts(context.Background(), canned(out), PullRequest{}, testSegs(), defaultBudget)
	if len(g.Cohorts[0].Files) != 1 || g.Cohorts[0].Files[0].Path != "core/auth.go" {
		t.Errorf("cohort A: %v", g.Cohorts[0].Files)
	}
	for _, f := range g.Cohorts[1].Files {
		if f.Path == "core/auth.go" {
			t.Error("duplicate not removed from later cohort")
		}
	}
}

func TestRepairPathNormalization(t *testing.T) {
	out := "{\"cohorts\": [{\"name\": \"A\", \"summary\": \"s\", \"files\": [\"b/core/auth.go\", \"`core/session.go`\", \"./docs/README.md\"]}]}"
	g := groupCohorts(context.Background(), canned(out), PullRequest{}, testSegs(), defaultBudget)
	if len(g.Cohorts) != 1 || len(g.Cohorts[0].Files) != 3 {
		t.Fatalf("normalization failed: %+v", g.Cohorts)
	}
}

func TestRepairPreservesFileOrder(t *testing.T) {
	// The within-cohort reading-order prompt rule relies on repair keeping the
	// model-emitted file order — pin that invariant.
	out := `{"cohorts": [{"name": "A", "summary": "s",
		"files": ["docs/README.md", "core/session.go", "core/auth.go"]}]}`
	g := groupCohorts(context.Background(), canned(out), PullRequest{}, testSegs(), defaultBudget)
	want := []string{"docs/README.md", "core/session.go", "core/auth.go"}
	if len(g.Cohorts) != 1 || len(g.Cohorts[0].Files) != 3 {
		t.Fatalf("got %+v", g.Cohorts)
	}
	for i, p := range want {
		if g.Cohorts[0].Files[i].Path != p {
			t.Fatalf("file order not preserved: got %v, want %v", g.Cohorts[0].Files, want)
		}
	}
}

func TestRepairPathTabSuffix(t *testing.T) {
	// A model may echo whole changed-files lines (status and/or count columns)
	// instead of the bare path — every tab-separated field is tried.
	out := "{\"cohorts\": [{\"name\": \"A\", \"summary\": \"s\", \"files\": [\"core/auth.go\\t+1/-1\", \"M\\tcore/session.go\\t+9/-2\", \"docs/README.md\"]}]}"
	g := groupCohorts(context.Background(), canned(out), PullRequest{}, testSegs(), defaultBudget)
	if len(g.Cohorts) != 1 || len(g.Cohorts[0].Files) != 3 {
		t.Fatalf("tab-suffixed paths not resolved: %+v", g.Cohorts)
	}
}

// --- hunk-level repair ---

// refString flattens a FileRef for compact assertions: "path" or "path#1,3".
func refString(r FileRef) string {
	if r.Hunks == nil {
		return r.Path
	}
	parts := make([]string, len(r.Hunks))
	for i, n := range r.Hunks {
		parts[i] = fmt.Sprint(n)
	}
	return r.Path + "#" + strings.Join(parts, ",")
}

func cohortRefs(c Cohort) []string {
	out := make([]string, len(c.Files))
	for i, r := range c.Files {
		out[i] = refString(r)
	}
	return out
}

func groupSplit(t *testing.T, out string) Grouping {
	t.Helper()
	return groupCohorts(context.Background(), canned(out), PullRequest{}, splitSegs(), defaultBudget)
}

func assertRefs(t *testing.T, c Cohort, want ...string) {
	t.Helper()
	got := cohortRefs(c)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("cohort %q refs = %v, want %v", c.Name, got, want)
	}
}

func TestRepairSplitAcrossCohorts(t *testing.T) {
	out := `{"cohorts": [
		{"name": "A", "summary": "s", "files": ["core/auth.go#1", "core/auth.go#3"]},
		{"name": "B", "summary": "s", "files": ["core/auth.go#2", "core/util.go"]}]}`
	g := groupSplit(t, out)
	if len(g.Cohorts) != 3 {
		t.Fatalf("got %+v", g.Cohorts)
	}
	assertRefs(t, g.Cohorts[0], "core/auth.go#1,3")
	assertRefs(t, g.Cohorts[1], "core/auth.go#2", "core/util.go")
	assertRefs(t, g.Cohorts[2], "img.png") // unclaimed binary swept to Other
}

func TestRepairBareClaimsRemainder(t *testing.T) {
	out := `{"cohorts": [
		{"name": "A", "summary": "s", "files": ["core/auth.go#2"]},
		{"name": "B", "summary": "s", "files": ["core/auth.go", "core/util.go", "img.png"]}]}`
	g := groupSplit(t, out)
	assertRefs(t, g.Cohorts[1], "core/auth.go#1,3", "core/util.go", "img.png")
}

func TestRepairFirstWinsAcrossCohorts(t *testing.T) {
	out := `{"cohorts": [
		{"name": "A", "summary": "s", "files": ["core/auth.go"]},
		{"name": "B", "summary": "s", "files": ["core/auth.go#2", "core/util.go", "img.png"]}]}`
	g := groupSplit(t, out)
	assertRefs(t, g.Cohorts[0], "core/auth.go")
	// The already-claimed #2 is silently skipped, like duplicate paths today.
	assertRefs(t, g.Cohorts[1], "core/util.go", "img.png")
}

func TestRepairSameCohortMergeSortNormalize(t *testing.T) {
	out := `{"cohorts": [{"name": "A", "summary": "s",
		"files": ["core/auth.go#3", "core/util.go#1", "core/auth.go#1", "core/auth.go#2"]}]}`
	g := groupSplit(t, out)
	// auth's claims merge at its first position and cover all 3 hunks → bare.
	assertRefs(t, g.Cohorts[0], "core/auth.go", "core/util.go#1")
	assertRefs(t, g.Cohorts[len(g.Cohorts)-1], "core/util.go#2", "img.png")
}

func TestRepairLeftoverHunksToOther(t *testing.T) {
	out := `{"cohorts": [{"name": "A", "summary": "s", "files": ["core/auth.go#1"]}]}`
	g := groupSplit(t, out)
	last := g.Cohorts[len(g.Cohorts)-1]
	if last.Name != "Other" {
		t.Fatalf("got %+v", g.Cohorts)
	}
	assertRefs(t, last, "core/auth.go#2,3", "core/util.go", "img.png")
}

func TestRepairInvalidHunkRefDropped(t *testing.T) {
	out := `{"cohorts": [{"name": "A", "summary": "s",
		"files": ["core/auth.go#9", "img.png#1", "core/util.go"]}]}`
	g := groupSplit(t, out)
	assertRefs(t, g.Cohorts[0], "core/util.go")
	assertRefs(t, g.Cohorts[len(g.Cohorts)-1], "core/auth.go", "img.png")
}

// TestRepairHunkPartition pins the core invariant: whatever the model emits,
// every hunk of every file (and every bare-only file) lands in exactly one
// cohort's refs, counting Other.
func TestRepairHunkPartition(t *testing.T) {
	outputs := []string{
		`{"cohorts": [{"name": "A", "summary": "s", "files": ["core/auth.go#1", "core/auth.go#3"]},
			{"name": "B", "summary": "s", "files": ["core/auth.go#2", "core/util.go"]}]}`,
		`{"cohorts": [{"name": "A", "summary": "s", "files": ["core/auth.go#2", "core/auth.go#2", "core/auth.go"]}]}`,
		`{"cohorts": [{"name": "A", "summary": "s", "files": ["core/util.go", "core/util.go#1", "junk.go", "img.png#3"]}]}`,
		`{"cohorts": [{"name": "A", "summary": "s", "files": ["img.png", "img.png"]},
			{"name": "B", "summary": "s", "files": ["core/auth.go#3"]}]}`,
		`{"cohorts": []}`,
	}
	idx := indexSegments(splitSegs())
	for _, out := range outputs {
		g := groupSplit(t, out)
		counts := map[string]int{} // "path#n" or bare-only "path" → times claimed
		for _, c := range g.Cohorts {
			for _, r := range c.Files {
				m := idx.hunks[r.Path]
				switch {
				case m == 0:
					counts[r.Path]++
				case r.Hunks == nil:
					for n := 1; n <= m; n++ {
						counts[fmt.Sprintf("%s#%d", r.Path, n)]++
					}
				default:
					for _, n := range r.Hunks {
						counts[fmt.Sprintf("%s#%d", r.Path, n)]++
					}
				}
			}
		}
		for _, p := range idx.paths {
			m := idx.hunks[p]
			if m == 0 {
				if counts[p] != 1 {
					t.Errorf("output %.40q: bare-only %s claimed %d times", out, p, counts[p])
				}
				continue
			}
			for n := 1; n <= m; n++ {
				if k := fmt.Sprintf("%s#%d", p, n); counts[k] != 1 {
					t.Errorf("output %.40q: %s claimed %d times", out, k, counts[k])
				}
			}
		}
	}
}

func TestGroupCohortsRetryThenSuccess(t *testing.T) {
	g := groupCohorts(context.Background(), canned("not json at all", goodJSON), PullRequest{}, testSegs(), defaultBudget)
	if len(g.Cohorts) != 2 {
		t.Fatalf("retry did not recover: %+v", g)
	}
}

func TestGroupCohortsFallbackAfterTwoFailures(t *testing.T) {
	g := groupCohorts(context.Background(), canned("garbage", "more garbage"), PullRequest{}, testSegs(), defaultBudget)
	if len(g.Cohorts) != 1 || g.Cohorts[0].Name != "All changes" {
		t.Fatalf("expected fallback cohort, got %+v", g.Cohorts)
	}
	if len(g.Cohorts[0].Files) != 3 {
		t.Errorf("fallback must cover all files: %v", g.Cohorts[0].Files)
	}
}

func TestGroupCohortsErrorThenFallback(t *testing.T) {
	fail := func(ctx context.Context, prompt string) (string, error) {
		return "", fmt.Errorf("exec: claude not found")
	}
	g := groupCohorts(context.Background(), fail, PullRequest{}, testSegs(), defaultBudget)
	if len(g.Cohorts) != 1 || g.Cohorts[0].Name != "All changes" {
		t.Fatalf("expected fallback cohort, got %+v", g.Cohorts)
	}
}

func TestTypeAndClaimParsedValidated(t *testing.T) {
	out := `{"cohorts": [
		{"name": "A", "summary": "s", "claim": "is the decision sound?", "type": "nonfix", "files": ["core/auth.go"]},
		{"name": "B", "summary": "s", "claim": "b?", "type": "bogus", "files": ["core/session.go"]},
		{"name": "C", "summary": "s", "claim": "c?", "files": ["docs/README.md"]}]}`
	g := groupCohorts(context.Background(), canned(out), PullRequest{}, testSegs(), defaultBudget)
	if g.Cohorts[0].Claim != "is the decision sound?" || g.Cohorts[0].Type != "nonfix" {
		t.Errorf("cohort A: %+v", g.Cohorts[0])
	}
	if g.Cohorts[0].Summary != "s" {
		t.Errorf("summary lost: %+v", g.Cohorts[0])
	}
	if g.Cohorts[1].Type != "claim" {
		t.Errorf("bogus type not normalized to claim: %+v", g.Cohorts[1])
	}
	if g.Cohorts[2].Type != "claim" {
		t.Errorf("missing type not normalized to claim: %+v", g.Cohorts[2])
	}
}

func TestClaimTypeRulesInPrompt(t *testing.T) {
	p := buildCohortPrompt(promptInput{NameStatus: "M\ta.go\t+1/-0\n"}, "nonce")
	for _, frag := range []string{`"claim"`, `"type"`, "nonfix", "transition-period risk",
		"never force-fit", "review signal"} {
		if !strings.Contains(p, frag) {
			t.Errorf("prompt missing rule fragment %q", frag)
		}
	}
}
