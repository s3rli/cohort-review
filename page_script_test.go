package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// TestPageScriptParses shells `node --check` over the page's inline scripts —
// a JS typo in page.tmpl otherwise ships silently (no JS harness exists) and
// leaves the page stuck on "loading diff…". Skipped when node is absent.
func TestPageScriptParses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not in PATH")
	}
	page, err := renderPage(PullRequest{Title: "T"}, testGrouping(), "line-by-line")
	if err != nil {
		t.Fatal(err)
	}
	scripts := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllSubmatch(page, -1)
	if len(scripts) < 2 {
		t.Fatalf("expected at least 2 inline scripts, got %d", len(scripts))
	}
	for i, m := range scripts {
		js := filepath.Join(t.TempDir(), fmt.Sprintf("s%d.js", i))
		if err := os.WriteFile(js, m[1], 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(node, "--check", js).CombinedOutput(); err != nil {
			t.Fatalf("inline script %d does not parse: %v\n%s", i, err, out)
		}
	}
}
