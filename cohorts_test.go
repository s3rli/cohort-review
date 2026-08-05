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
	giant := seg("vendor/lock.json", "diff --git a/vendor/lock.json b/vendor/lock.json\n"+strings.Repeat("+x\n", 200))
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
	g := groupCohorts(context.Background(), canned(goodJSON), testSegs(), defaultBudget)
	if g.Walkthrough != "Adds auth." || len(g.Cohorts) != 2 {
		t.Fatalf("got %+v", g)
	}
}

func TestGroupCohortsFencedAndProseWrapped(t *testing.T) {
	for _, out := range []string{
		"```json\n" + goodJSON + "\n```",
		"Here is the grouping:\n" + goodJSON + "\nHope this helps!",
	} {
		g := groupCohorts(context.Background(), canned(out), testSegs(), defaultBudget)
		if len(g.Cohorts) != 2 {
			t.Errorf("output %q: got %d cohorts", out[:20], len(g.Cohorts))
		}
	}
}

func TestRepairHallucinatedDroppedMissedToOther(t *testing.T) {
	out := `{"walkthrough": "w", "cohorts": [
		{"name": "Auth", "summary": "s", "files": ["core/auth.go", "made/up.go"]}]}`
	g := groupCohorts(context.Background(), canned(out), testSegs(), defaultBudget)
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
	g := groupCohorts(context.Background(), canned(out), testSegs(), defaultBudget)
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
	g := groupCohorts(context.Background(), canned(out), testSegs(), defaultBudget)
	if len(g.Cohorts) != 1 || len(g.Cohorts[0].Files) != 3 {
		t.Fatalf("normalization failed: %+v", g.Cohorts)
	}
}

func TestGroupCohortsRetryThenSuccess(t *testing.T) {
	g := groupCohorts(context.Background(), canned("not json at all", goodJSON), testSegs(), defaultBudget)
	if len(g.Cohorts) != 2 {
		t.Fatalf("retry did not recover: %+v", g)
	}
}

func TestGroupCohortsFallbackAfterTwoFailures(t *testing.T) {
	g := groupCohorts(context.Background(), canned("garbage", "more garbage"), testSegs(), defaultBudget)
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
	g := groupCohorts(context.Background(), fail, testSegs(), defaultBudget)
	if len(g.Cohorts) != 1 || g.Cohorts[0].Name != "All changes" {
		t.Fatalf("expected fallback cohort, got %+v", g.Cohorts)
	}
}
