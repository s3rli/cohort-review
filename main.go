// cohort-review: fetch a Bitbucket PR's diff, group changed files into named
// cohorts with one LLM call, and serve an interactive diff2html review page on
// localhost. Read-only — never posts anything back to the PR.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// defaultPort is fixed rather than ephemeral because the page keeps its
// settings — theme, layout, sidebar width, and which files are marked Viewed —
// in localStorage, which is scoped to the origin, and the origin includes the
// port. A random port every run silently discarded all of it.
const defaultPort = 7717

func run(args []string) int {
	fs := flag.NewFlagSet("cohort-review", flag.ExitOnError)
	port := fs.Int("port", defaultPort, "listen port (0 = any free port)")
	noOpen := fs.Bool("no-open", false, "don't open the browser")
	model := fs.String("model", "sonnet", "claude model for grouping")
	budget := fs.Int("budget", defaultBudget, "max chars of diff hunks sent to the model")
	format := fs.String("format", "line-by-line", "initial diff layout: line-by-line | side-by-side")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: cohort-review [flags] <https://bitbucket.org/{ws}/{repo}/pull-requests/{n}>\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	if *format != "line-by-line" && *format != "side-by-side" {
		fmt.Fprintf(os.Stderr, "cohort-review: invalid --format %q (line-by-line | side-by-side)\n", *format)
		return 2
	}
	ref, err := parsePRRef(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "cohort-review: %v\n", err)
		return 2
	}

	loadDotEnv(".env")
	pairs := credentialPairs()
	if len(pairs) == 0 {
		fmt.Fprintln(os.Stderr, "cohort-review: bitbucket credentials missing: set BITBUCKET_USERNAME + BITBUCKET_APP_PASSWORD, or BITBUCKET_EMAIL + BITBUCKET_API_TOKEN, in the environment or ./.env")
		return 2
	}
	claudeMissing := false
	if _, err := exec.LookPath("claude"); err != nil {
		// Not fatal: grouping degrades to the single-cohort fallback.
		claudeMissing = true
		fmt.Fprintln(os.Stderr, "cohort-review: claude CLI not found in PATH — install Claude Code and log in (or set CLAUDE_CODE_OAUTH_TOKEN); showing all files in one cohort")
	}

	group := func(ctx context.Context, prompt string) (string, error) {
		return runClaude(ctx, *model, prompt)
	}
	if claudeMissing {
		group = func(context.Context, string) (string, error) {
			return "", errors.New("claude CLI not in PATH")
		}
	}
	// Try each credential pair until one authenticates; a 401 fails fast on
	// the PR fetch, before any grouping work.
	var a *app
	var loadErr error
	for i, c := range pairs {
		a = &app{
			client: NewClient(c.user, c.password, ""),
			group:  group,
			model:  *model,
			budget: *budget,
			format: *format,
		}
		loadErr = a.load(context.Background(), ref)
		if !errors.Is(loadErr, errUnauthorized) {
			break
		}
		if i+1 < len(pairs) {
			fmt.Fprintf(os.Stderr, "cohort-review: auth failed with %s, trying %s…\n", c.label, pairs[i+1].label)
		}
	}
	if loadErr != nil {
		return fatalFetch(loadErr, fs.Arg(0))
	}
	return serve(a, *port, !*noOpen)
}

type creds struct{ label, user, password string }

// credentialPairs returns the configured Bitbucket basic-auth candidates in
// preference order. Bitbucket Cloud deprecated app passwords, so environments
// often carry a working Atlassian email + API token next to a stale
// username + app-password pair — both are tried.
func credentialPairs() []creds {
	var out []creds
	if u, p := os.Getenv("BITBUCKET_USERNAME"), os.Getenv("BITBUCKET_APP_PASSWORD"); u != "" && p != "" {
		out = append(out, creds{"BITBUCKET_USERNAME + BITBUCKET_APP_PASSWORD", u, p})
	}
	if u, p := os.Getenv("BITBUCKET_EMAIL"), os.Getenv("BITBUCKET_API_TOKEN"); u != "" && p != "" {
		out = append(out, creds{"BITBUCKET_EMAIL + BITBUCKET_API_TOKEN", u, p})
	}
	return out
}

// fatalFetch maps Bitbucket fetch errors to actionable messages.
func fatalFetch(err error, url string) int {
	switch {
	case errors.Is(err, errUnauthorized):
		fmt.Fprintln(os.Stderr, "cohort-review: bitbucket auth failed (check BITBUCKET_USERNAME/BITBUCKET_APP_PASSWORD or BITBUCKET_EMAIL/BITBUCKET_API_TOKEN, and repo access)")
	case errors.Is(err, errNotFound):
		fmt.Fprintf(os.Stderr, "cohort-review: PR not found: %s\n", url)
	default:
		fmt.Fprintf(os.Stderr, "cohort-review: %v\n", err)
	}
	return 1
}

// loadDotEnv is copied from code-review-agent cmd/benchrunner/main.go — keep
// parity for convergence. KEY=VALUE lines, # comments skipped, existing env wins.
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || os.Getenv(strings.TrimSpace(k)) != "" {
			continue
		}
		os.Setenv(strings.TrimSpace(k), strings.TrimSpace(v))
	}
}
