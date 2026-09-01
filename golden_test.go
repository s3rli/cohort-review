//go:build golden

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestGolden replays the stored golden PRs through the real grouping call
// (claude CLI; diff and metadata from disk, no network) and prints actual vs
// expected for HUMAN judgment (handoff W6: 判分不用全自動). It fails only on
// harness errors, never on grouping content. Run via `make golden`.
func TestGolden(t *testing.T) {
	entries, err := os.ReadDir("testdata/golden")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "expected" {
			fixtures = append(fixtures, filepath.Join("testdata/golden", e.Name()))
		}
	}
	if len(fixtures) == 0 {
		t.Fatal("no golden fixtures — run scripts/fetch-golden.sh first")
	}
	for _, dir := range fixtures {
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, "pr.json"))
			if err != nil {
				t.Fatal(err)
			}
			var pr struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			}
			if err := json.Unmarshal(raw, &pr); err != nil {
				t.Fatal(err)
			}
			diff, err := os.ReadFile(filepath.Join(dir, "diff.txt"))
			if err != nil {
				t.Fatal(err)
			}
			segs := SplitUnifiedDiff(string(diff))
			group := func(ctx context.Context, prompt string) (string, error) {
				fmt.Printf("[%s] prompt: %d KB total\n", name, len(prompt)/1024)
				return runClaude(ctx, "sonnet", prompt)
			}
			g := groupCohorts(context.Background(), group,
				PullRequest{Title: pr.Title, Description: pr.Description}, segs, defaultBudget)
			fmt.Printf("\n===== %s: ACTUAL (%d cohorts, %d lines folded, degraded=%v) =====\n",
				name, len(g.Cohorts), g.FoldedLines, g.Degraded)
			for i, c := range g.Cohorts {
				fmt.Printf("%d. [%s] %s — %s\n   claim: %s\n", i+1, c.Type, c.Name, c.Summary, c.Claim)
				for _, f := range c.Files {
					fmt.Printf("   - %s\n", refString(f))
				}
			}
			if exp, err := os.ReadFile(filepath.Join("testdata/golden/expected", name+".md")); err == nil {
				fmt.Printf("===== %s: EXPECTED =====\n%s\n", name, exp)
			} else {
				fmt.Printf("===== %s: NO EXPECTED FILE (%v) =====\n", name, err)
			}
		})
	}
}
