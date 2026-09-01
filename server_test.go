package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func testGrouping() Grouping {
	return Grouping{
		Walkthrough: "Does things.",
		Cohorts:     []Cohort{{Name: "Core", Summary: "the core", Claim: "does it work?", Type: "claim", Files: []FileRef{{Path: "a.go"}}}},
	}
}

func getBody(t *testing.T, srv *httptest.Server, path string) (int, string) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestServerEndpoints(t *testing.T) {
	// A diff containing a literal </script> must survive byte-for-byte — the
	// reason the diff is served as its own resource instead of inlined.
	diff := "diff --git a/x.html b/x.html\n--- a/x.html\n+++ b/x.html\n@@ -1 +1 @@\n-<p>\n+</script><script>alert(1)</script>\n"
	page, err := renderPage(PullRequest{Title: "My PR", WebURL: "https://bitbucket.org/ws/repo/pull-requests/1"}, testGrouping(), "line-by-line")
	if err != nil {
		t.Fatal(err)
	}
	a := &app{page: page, diff: diff}
	srv := httptest.NewServer(a.mux())
	defer srv.Close()

	code, home := getBody(t, srv, "/")
	if code != 200 || !strings.Contains(home, "My PR") {
		t.Errorf("/: code %d, title present: %v", code, strings.Contains(home, "My PR"))
	}

	code, gotDiff := getBody(t, srv, "/diff.txt")
	if code != 200 || gotDiff != diff {
		t.Errorf("/diff.txt: code %d, byte-equal: %v", code, gotDiff == diff)
	}

	for _, path := range []string{"/assets/diff2html.min.js", "/assets/diff2html.min.css",
		"/assets/diff2html-ui.min.js", "/assets/github.min.css", "/assets/github-dark.min.css"} {
		if code, _ := getBody(t, srv, path); code != 200 {
			t.Errorf("%s: code %d", path, code)
		}
	}

	if code, _ := getBody(t, srv, "/nope"); code != 404 {
		t.Errorf("/nope: code %d, want 404", code)
	}
}

// fakeBitbucket serves the two API endpoints load() hits.
func fakeBitbucket(t *testing.T, title, desc, diff string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /2.0/repositories/{ws}/{repo}/pullrequests/{n}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"title": %q, "description": %q, "links": {"html": {"href": "https://bitbucket.org/x"}}}`, title, desc)
	})
	mux.HandleFunc("GET /2.0/repositories/{ws}/{repo}/pullrequests/{n}/diff", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, diff)
	})
	return httptest.NewServer(mux)
}

func TestLoadSwitchesPR(t *testing.T) {
	newDiff := "diff --git a/new.go b/new.go\n--- a/new.go\n+++ b/new.go\n@@ -1 +1 @@\n-a\n+b\n"
	bb := fakeBitbucket(t, "Second PR", "", newDiff)
	defer bb.Close()

	a := &app{
		client: NewClient("u", "p", bb.URL),
		group: func(ctx context.Context, prompt string) (string, error) {
			return `{"walkthrough": "w", "cohorts": [{"name": "N", "summary": "s", "files": ["new.go"]}]}`, nil
		},
		format: "line-by-line",
		page:   []byte("old page"),
		diff:   "old diff",
	}
	srv := httptest.NewServer(a.mux())
	defer srv.Close()

	code, body := getBody(t, srv, "/load?pr=https://bitbucket.org/ws/repo/pull-requests/2")
	// The test client follows the 303 redirect back to /.
	if code != 200 || !strings.Contains(body, "Second PR") {
		t.Fatalf("/load: code %d, new title present: %v", code, strings.Contains(body, "Second PR"))
	}
	if code, gotDiff := getBody(t, srv, "/diff.txt"); code != 200 || gotDiff != newDiff {
		t.Errorf("/diff.txt not swapped: %q", gotDiff)
	}
}

func TestLoadThreadsPRMetadataIntoPrompt(t *testing.T) {
	diff := "diff --git a/new.go b/new.go\n--- a/new.go\n+++ b/new.go\n@@ -1 +1 @@\n-a\n+b\n"
	bb := fakeBitbucket(t, "My PR", "Detailed intent here.", diff)
	defer bb.Close()

	var gotPrompt string
	a := &app{
		client: NewClient("u", "p", bb.URL),
		group: func(ctx context.Context, prompt string) (string, error) {
			gotPrompt = prompt
			return `{"walkthrough": "w", "cohorts": [{"name": "N", "summary": "s", "files": ["new.go"]}]}`, nil
		},
		budget: defaultBudget,
		format: "line-by-line",
	}
	if err := a.load(context.Background(), PRRef{Workspace: "ws", Repo: "repo", Number: 1}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPrompt, "## PR title and description") ||
		!strings.Contains(gotPrompt, "My PR") || !strings.Contains(gotPrompt, "Detailed intent here.") {
		t.Error("PR metadata did not reach the grouping prompt")
	}
}

func TestLoadBadURLKeepsState(t *testing.T) {
	a := &app{page: []byte("current page"), diff: "current diff"}
	srv := httptest.NewServer(a.mux())
	defer srv.Close()

	if code, _ := getBody(t, srv, "/load?pr=https://github.com/x/y/pull/1"); code != 400 {
		t.Errorf("/load bad url: code %d, want 400", code)
	}
	if _, body := getBody(t, srv, "/"); body != "current page" {
		t.Error("state clobbered by failed load")
	}
}

func TestLoadFetchFailureKeepsState(t *testing.T) {
	bb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer bb.Close()

	a := &app{client: NewClient("u", "p", bb.URL), page: []byte("current page"), diff: "current diff"}
	srv := httptest.NewServer(a.mux())
	defer srv.Close()

	code, body := getBody(t, srv, "/load?pr=https://bitbucket.org/ws/repo/pull-requests/9")
	if code != 404 || !strings.Contains(body, "previous PR is still served") {
		t.Errorf("/load fetch failure: code %d body %q", code, body)
	}
	if _, home := getBody(t, srv, "/"); home != "current page" {
		t.Error("state clobbered by failed load")
	}
}

func TestRenderPageDataRoundTrips(t *testing.T) {
	g := Grouping{
		Walkthrough: "Does things.",
		Cohorts: []Cohort{
			{Name: "Core", Summary: "the core", Claim: "does it work?", Type: "claim", Files: []FileRef{{Path: "a.go"}}},
			{Name: "Dels", Summary: "old suite", Claim: "is the removal complete?", Type: "deletion",
				Files: []FileRef{{Path: "old/x.go"}, {Path: "old/y.go"}}},
		},
	}
	page, err := renderPage(PullRequest{Title: "T", WebURL: "u"}, g, "side-by-side")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`const PAGE = (.*);`).FindSubmatch(page)
	if m == nil {
		t.Fatal("const PAGE not found in rendered page")
	}
	var got pageData
	if err := json.Unmarshal(m[1], &got); err != nil {
		t.Fatalf("PAGE is not valid JSON: %v", err)
	}
	if got.Title != "T" || got.Format != "side-by-side" || len(got.Cohorts) != 2 || got.Cohorts[0].Files[0].Path != "a.go" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.Cohorts[0].Claim != "does it work?" || got.Cohorts[0].Type != "claim" {
		t.Errorf("claim/type lost: %+v", got.Cohorts[0])
	}
	if got.Cohorts[1].Type != "deletion" {
		t.Errorf("type lost: %+v", got.Cohorts[1])
	}
}

func TestRenderPageEscapesHostileTitle(t *testing.T) {
	hostile := `</script><script>alert(1)</script>`
	page, err := renderPage(PullRequest{Title: hostile}, testGrouping(), "line-by-line")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(page), hostile) {
		t.Error("hostile title reached the page unescaped")
	}
}

func TestLoadWritesRunRecord(t *testing.T) {
	diff := "diff --git a/new.go b/new.go\n--- a/new.go\n+++ b/new.go\n@@ -1 +1 @@\n-a\n+b\n"
	bb := fakeBitbucket(t, "T", "- bullet\n", diff)
	defer bb.Close()
	mpath := filepath.Join(t.TempDir(), "runs.jsonl")
	a := &app{
		client: NewClient("u", "p", bb.URL),
		group: func(context.Context, string) (string, error) {
			return `{"walkthrough": "w", "cohorts": [{"name": "N", "summary": "s", "claim": "q?", "type": "claim", "files": ["new.go"]}]}`, nil
		},
		budget:  defaultBudget,
		format:  "line-by-line",
		metrics: mpath,
	}
	if err := a.load(context.Background(), PRRef{Workspace: "ws", Repo: "r", Number: 1}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(mpath)
	if err != nil {
		t.Fatal(err)
	}
	var r runRecord
	if err := json.Unmarshal(bytes.TrimSpace(data), &r); err != nil {
		t.Fatalf("runs line invalid: %v", err)
	}
	if r.PR != "ws/r#1" || r.Files != 1 || r.Anchor == nil || r.Anchor.DescBullets != 1 {
		t.Errorf("%+v", r)
	}
	if r.GroupSecs <= 0 {
		t.Errorf("group_secs not recorded: %+v", r)
	}
}

func TestLoadWritesErrorRecordOnFetchFailure(t *testing.T) {
	bb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer bb.Close()
	mpath := filepath.Join(t.TempDir(), "runs.jsonl")
	a := &app{client: NewClient("u", "p", bb.URL), metrics: mpath}
	if err := a.load(context.Background(), PRRef{Workspace: "ws", Repo: "r", Number: 9}); err == nil {
		t.Fatal("expected load error")
	}
	data, err := os.ReadFile(mpath)
	if err != nil {
		t.Fatal(err)
	}
	var r runRecord
	if err := json.Unmarshal(bytes.TrimSpace(data), &r); err != nil {
		t.Fatalf("runs line invalid: %v", err)
	}
	if r.PR != "ws/r#9" || r.Error == "" {
		t.Errorf("failed run not recorded: %+v", r)
	}
}
