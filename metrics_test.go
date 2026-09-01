package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComputeRecord(t *testing.T) {
	// splitSegs churn: auth 3 hunks × 2 lines, util 2 × 2, img 0 → total 10.
	segs := splitSegs()
	g := Grouping{FoldedLines: 4, Cohorts: []Cohort{
		{Name: "Odd", Type: "misc", Files: []FileRef{{Path: "core/util.go", Hunks: []int{1}}}},
		{Name: "A", Type: "claim", Files: []FileRef{{Path: "core/auth.go"}}},
		{Name: "Unclassified (grouping fallback)", Files: []FileRef{{Path: "core/util.go", Hunks: []int{2}}, {Path: "img.png"}}},
	}}
	pr := PullRequest{Title: "PLAT-2328 fix callbacks", Description: "Does:\n- adds a thing\n- removes another\n"}
	ref := PRRef{Workspace: "ws", Repo: "repo", Number: 18}
	r := computeRecord(ref, "sonnet", pr, g, segs, 23.4)
	if r.PR != "ws/repo#18" || r.Model != "sonnet" || r.GroupSecs != 23.4 {
		t.Errorf("identity: %+v", r)
	}
	if r.Lines != 10 || r.Files != 3 || r.Cohorts != 3 {
		t.Errorf("totals: %+v", r)
	}
	// misc pct: semantic misc only (util hunk 1 = 2 lines of 10), NOT the fallback bucket.
	if r.MiscLinesPct != 20 {
		t.Errorf("misc_lines_pct = %v, want 20", r.MiscLinesPct)
	}
	if r.FoldedLinesPct != 40 {
		t.Errorf("folded_lines_pct = %v, want 40", r.FoldedLinesPct)
	}
	if r.Types["misc"] != 1 || r.Types["claim"] != 1 || r.Types["unclassified"] != 1 {
		t.Errorf("types: %v", r.Types)
	}
	if r.Anchor == nil || r.Anchor.DescChars != len(pr.Description) ||
		r.Anchor.DescBullets != 2 || r.Anchor.JiraKey != "PLAT-2328" {
		t.Errorf("anchor: %+v", r.Anchor)
	}
	if r.Error != "" {
		t.Errorf("unexpected error field: %q", r.Error)
	}

	g.Degraded = true
	if d := computeRecord(ref, "sonnet", pr, g, segs, 1); d.Error == "" {
		t.Error("degraded run must carry an error field")
	}

	cjk := computeRecord(ref, "m", PullRequest{Description: "三行摘要"}, g, segs, 1)
	if cjk.Anchor.DescChars != 4 {
		t.Errorf("desc_chars must count runes, got %d", cjk.Anchor.DescChars)
	}
}

func TestComputeRecordChurnEdges(t *testing.T) {
	// Non-uniform hunks so a hunk-index slip is visible, and a "+++"-looking
	// added line (diff-of-a-diff) that must not be counted.
	segs := []FileSegment{seg("v.go",
		"diff --git a/v.go b/v.go\n@@ -1 +1 @@\n-a\n+b\n@@ -9,2 +9,4 @@\n ctx\n-c\n+d\n+++ e\n")}
	g := Grouping{Cohorts: []Cohort{{Name: "m", Type: "misc", Files: []FileRef{{Path: "v.go", Hunks: []int{2}}}}}}
	r := computeRecord(PRRef{"ws", "r", 1}, "m", PullRequest{}, g, segs, 1)
	if r.Lines != 4 || r.MiscLinesPct != 50 {
		t.Errorf("lines=%d misc=%v, want 4 / 50", r.Lines, r.MiscLinesPct)
	}
	// A path-bearing segment with no hunks must not divide by zero: an
	// unguarded NaN makes the whole record unmarshalable and it vanishes.
	z := computeRecord(PRRef{"ws", "r", 2}, "m", PullRequest{},
		Grouping{Cohorts: []Cohort{{Type: "misc", Files: []FileRef{{Path: "x.bin"}}}}},
		[]FileSegment{seg("x.bin", "diff --git a/x.bin b/x.bin\nBinary files differ\n")}, 1)
	if _, err := json.Marshal(z); err != nil {
		t.Fatalf("zero-churn record unmarshalable: %v", err)
	}

	// A path in two segments is bare-only (indexSegments), so its misc churn
	// must sum BOTH segments, not just one.
	dup := []FileSegment{
		seg("d.go", "diff --git a/d.go b/d.go\n@@ -1 +1 @@\n-a\n+b\n"),
		seg("d.go", "diff --git a/d.go b/d.go\n@@ -5 +5 @@\n-c\n+d\n"),
	}
	dr := computeRecord(PRRef{"ws", "r", 3}, "m", PullRequest{},
		Grouping{Cohorts: []Cohort{{Type: "misc", Files: []FileRef{{Path: "d.go"}}}}}, dup, 1)
	if dr.Lines != 4 || dr.Files != 1 || dr.MiscLinesPct != 100 {
		t.Errorf("dup path: lines=%d files=%d misc=%v, want 4 / 1 / 100", dr.Lines, dr.Files, dr.MiscLinesPct)
	}
}

func TestAppendJSONL(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "runs.jsonl")
	for i := 0; i < 2; i++ {
		if err := appendJSONL(p, map[string]int{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines: %q", len(lines), data)
	}
	var v struct {
		I int `json:"i"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &v); err != nil || v.I != 1 {
		t.Errorf("line 2 = %q (err %v)", lines[1], err)
	}
}
