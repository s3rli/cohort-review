package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	page, err := renderPage(PullRequest{Title: "T"}, testGrouping(), "line-by-line", 1)
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

// TestSplitDiffBlocksMatchesGo runs the page's own diff-splitting code in node
// over both diff styles and checks it indexes exactly the paths
// SplitUnifiedDiff does. The two are the server and client halves of the same
// exactly-once invariant. Skipped when node is absent.
func TestSplitDiffBlocksMatchesGo(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not in PATH")
	}
	page, err := renderPage(PullRequest{Title: "T"}, testGrouping(), "line-by-line", 1)
	if err != nil {
		t.Fatal(err)
	}
	// The page splits inline; lift those two lines into a function so the very
	// same code under test runs here.
	fn := regexp.MustCompile(`(?m)^\s*const useGitHeaders = .*\n\s*const blocks = .*$`).Find(page)
	if fn == nil {
		t.Fatal("diff-splitting lines not found in the rendered page")
	}
	diffs := map[string]string{
		"git headers": "diff --git a/one.go b/one.go\n--- a/one.go\n+++ b/one.go\n@@ -1 +1 @@\n-a\n+b\n" +
			"diff --git a/two.go b/two.go\n--- a/two.go\n+++ b/two.go\n@@ -1 +1 @@\n-c\n+d\n",
		"no git headers": "--- a/one.go\n+++ b/one.go\n@@ -1 +1 @@\n-a\n+b\n" +
			"--- a/two.go\n+++ b/two.go\n@@ -1 +1 @@\n-c\n+d\n",
	}
	for name, diff := range diffs {
		var want []string
		for _, sg := range SplitUnifiedDiff(diff) {
			if sg.Path != "" {
				want = append(want, sg.Path)
			}
		}
		harness := "function splitDiffBlocks(rawDiff) {\n" + string(fn) + "\n  return blocks;\n}\n" +
			"const raw = " + strconv.Quote(diff) + `;
const paths = [];
splitDiffBlocks(raw).forEach(b => {
  const m = b.match(/^\+\+\+ b\/(.+)$/m) || b.match(/^--- a\/(.+)$/m);
  if (m) paths.push(m[1]);
});
console.log(paths.join(","));
`
		js := filepath.Join(t.TempDir(), "h.js")
		if err := os.WriteFile(js, []byte(harness), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(node, js).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: node failed: %v\n%s", name, err, out)
		}
		if got := strings.TrimSpace(string(out)); got != strings.Join(want, ",") {
			t.Errorf("%s: page indexed %q, Go indexed %q", name, got, strings.Join(want, ","))
		}
	}
}
