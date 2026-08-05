package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
)

//go:embed assets/diff2html.min.js assets/diff2html.min.css
var assetsFS embed.FS

//go:embed page.tmpl
var pageTmpl string

// app holds everything needed to load a PR plus the currently served state,
// so /load can swap to another PR while the server runs.
type app struct {
	client *Client
	group  groupFunc
	model  string
	budget int
	format string

	mu   sync.RWMutex
	page []byte
	diff string
}

// load fetches a PR, groups its diff, renders the page, and atomically swaps
// the served state. On error the previous state stays untouched.
func (a *app) load(ctx context.Context, ref PRRef) error {
	fmt.Fprintf(os.Stderr, "fetching PR #%d from %s/%s…\n", ref.Number, ref.Workspace, ref.Repo)
	pr, err := a.client.FetchPR(ctx, ref)
	if err != nil {
		return err
	}
	diff, err := a.client.FetchDiff(ctx, ref)
	if err != nil {
		return err
	}
	segs := SplitUnifiedDiff(diff)
	paths := segmentPaths(segs)
	if len(paths) == 0 {
		return errors.New("PR has no diff to review")
	}
	fmt.Fprintf(os.Stderr, "diff: %d files, %d KB\n", len(paths), len(diff)/1024)
	fmt.Fprintf(os.Stderr, "grouping into cohorts (model %s)…\n", a.model)
	g := groupCohorts(ctx, a.group, pr, segs, a.budget)
	fmt.Fprintf(os.Stderr, "%d cohorts\n", len(g.Cohorts))
	page, err := renderPage(pr, g, a.format)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.page, a.diff = page, diff
	a.mu.Unlock()
	return nil
}

// pageData is injected into the page as `const PAGE = …` — html/template's JS
// autoescaper JSON-encodes it and escapes `<`, so PR-controlled text (title,
// summaries) can't break out of the script context.
type pageData struct {
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	Walkthrough string   `json:"walkthrough"`
	Format      string   `json:"format"`
	Cohorts     []Cohort `json:"cohorts"`
}

func renderPage(pr PullRequest, g Grouping, format string) ([]byte, error) {
	t, err := template.New("page").Parse(pageTmpl)
	if err != nil {
		return nil, fmt.Errorf("parse page template: %w", err)
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]any{
		"Title": pr.Title,
		"Page": pageData{
			Title:       pr.Title,
			URL:         pr.WebURL,
			Walkthrough: g.Walkthrough,
			Format:      format,
			Cohorts:     g.Cohorts,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("render page: %w", err)
	}
	return buf.Bytes(), nil
}

// mux serves the current page, the raw diff, the vendored assets, and /load.
// The diff is served as its own resource (fetched by the page's JS) instead of
// being inlined in the HTML: a diff can contain a literal </script>, which
// would terminate any inline block; a plain-text response has no escaping
// surface at all.
func (a *app) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		a.mu.RLock()
		page := a.page
		a.mu.RUnlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	mux.HandleFunc("/diff.txt", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		diff := a.diff
		a.mu.RUnlock()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(diff))
	})
	mux.HandleFunc("/load", a.handleLoad)
	mux.Handle("/assets/", http.FileServer(http.FS(assetsFS)))
	return mux
}

// handleLoad switches the server to another PR: /load?pr=<bitbucket-pr-url>.
// Synchronous — the browser waits out the grouping call, then is redirected
// back to /. On any failure the currently served PR is left as-is.
func (a *app) handleLoad(w http.ResponseWriter, r *http.Request) {
	ref, err := parsePRRef(r.URL.Query().Get("pr"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.load(r.Context(), ref); err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, errNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, fmt.Sprintf("failed to load PR: %v\n\nthe previous PR is still served at /", err), status)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func serve(a *app, port int, openBrowser bool) int {
	// 127.0.0.1 only: the diff may be proprietary code — never bind 0.0.0.0.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "cohort-review: listen: %v\n", err)
		return 1
	}
	url := fmt.Sprintf("http://%s", ln.Addr())
	fmt.Fprintf(os.Stderr, "serving %s (Ctrl-C to quit)\n", url)
	if openBrowser {
		if err := exec.Command("open", url).Start(); err != nil {
			fmt.Fprintf(os.Stderr, "cohort-review: could not open browser: %v\n", err)
		}
	}
	if err := http.Serve(ln, a.mux()); err != nil {
		fmt.Fprintf(os.Stderr, "cohort-review: serve: %v\n", err)
		return 1
	}
	return 0
}
