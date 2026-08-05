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

// --- budget assembly ---

func TestBuildPromptInputUnderBudget(t *testing.T) {
	in := buildPromptInput(testSegs(), defaultBudget)
	if len(in.Omitted) != 0 {
		t.Errorf("unexpected omissions: %v", in.Omitted)
	}
	for _, p := range []string{"core/auth.go", "core/session.go", "docs/README.md"} {
		if !strings.Contains(in.Hunks, p) {
			t.Errorf("hunks missing %s", p)
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
	budget := len(testSegs()[0].Raw) + len(testSegs()[1].Raw) + len(testSegs()[2].Raw)
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
}

func TestBuildPromptInputTruncationFallsBackToOmitted(t *testing.T) {
	header := "diff --git a/big.go b/big.go\n"
	hunk1 := "@@ -1 +1 @@\n-first_hunk_old\n+first_hunk_new\n"
	giant := seg("big.go", header+hunk1+"@@ -10,200 +10,200 @@\n"+strings.Repeat("+pad\n", 200))
	segs := append([]FileSegment{giant}, testSegs()...)
	// Cap too small for even header+first hunk: degrade to omission, as before
	// truncation existed.
	budget := 200
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
	{"name": "Auth", "summary": "core auth", "files": ["core/auth.go", "core/session.go"]},
	{"name": "Docs", "summary": "docs", "files": ["docs/README.md"]}]}`

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
	if g.Cohorts[0].Files[0] != "core/auth.go" || len(g.Cohorts[0].Files) != 1 {
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
	if len(g.Cohorts[0].Files) != 1 || g.Cohorts[0].Files[0] != "core/auth.go" {
		t.Errorf("cohort A: %v", g.Cohorts[0].Files)
	}
	for _, f := range g.Cohorts[1].Files {
		if f == "core/auth.go" {
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
		if g.Cohorts[0].Files[i] != p {
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
