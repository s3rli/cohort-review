# cohort-review

Local, read-only PR review aid: fetches a Bitbucket PR's diff, groups changed files into named cohorts with one LLM call, and serves an interactive [diff2html](https://diff2html.xyz) page on localhost. Never posts anything back to the PR.

![cohort-review reviewing a demo PR](docs/screenshot.png)

*A demo PR grouped into six cohorts — tests sit with the code they test, and `bitbucket.go` is split across two cohorts by hunk (the `(2/4)` badge). The sidebar opens the cohort you're reading; the pager steps through its files one at a time.*

## Setup

Requirements: Go 1.24+, the `claude` CLI in PATH (logged in, or `CLAUDE_CODE_OAUTH_TOKEN` set).

Credentials — in the environment or a `./.env` file, either pair (tried in this order):

```
BITBUCKET_USERNAME=<username>   BITBUCKET_APP_PASSWORD=<app password>
BITBUCKET_EMAIL=<email>         BITBUCKET_API_TOKEN=<Atlassian API token>
```

Install: `go install github.com/s3rli/cohort-review@latest` — or clone and `go build .`

## Usage

```
cohort-review https://bitbucket.org/{workspace}/{repo}/pull-requests/{id}
```

Fetches the diff, groups it into cohorts (~10–60s), prints a `http://127.0.0.1:<port>` URL, and opens your browser. Ctrl-C to quit.

| Flag | Default | |
|---|---|---|
| `--port` | `7717` | listen port; `0` picks any free port |
| `--no-open` | | don't open the browser |
| `--model` | `sonnet` | claude model for grouping |
| `--budget` | `204800` | max chars of diff hunks sent to the model |
| `--format` | `line-by-line` | initial layout, or `side-by-side` |

### In the page

Two ways to read the same diff, switched in the toolbar:

- **paged** (default) — one cohort, one file at a time. The sidebar picks the cohort and opens its file list; `‹ File N of M ▾ ›` steps through that cohort's files, and the caret lists them.
- **all cohorts** — every cohort in one scroll, which is what makes find-in-page work across the whole PR.

| | |
|---|---|
| line-by-line ↔ side-by-side | diff layout |
| expand (arrows icon, toolbar) | drops the sidebar and header; `Esc` restores |
| theme (half-circle icon, header) | dark/light |
| Viewed | per-file, remembered per PR |
| switch PR | paste another URL to load it in place (re-groups, ~10–60s) |

The current cohort and file are in the URL — `#paged/<cohort>/<file>`, or `#all/<cohort>` — so a link points at one file and back/forward step through both.

Layout, theme, expand and Viewed marks are kept in `localStorage`, which browsers scope to the origin — and the origin includes the port. That is why `--port` has a fixed default: on a different port none of it carries over. If the default port is busy the server falls back to a free one and says so.

## Notes

- Grouping never blocks the page: if the LLM call fails or returns garbage (retried once), everything renders under a single "All changes" cohort.
- A mixed-concern file can be split across cohorts by hunk — the sidebar shows a `(k/M)` hunk count and the full diff still renders exactly once.
- Rendering is deferred: paged mode draws only the file on screen, and all-cohorts mode draws what you scroll past, then fills in the rest while the browser is idle so find-in-page reaches everything. Large or generated files (lockfiles, minified assets, very large hunks) stay behind a `show large diff` placeholder — and so stay out of find-in-page until expanded.
- Diffs over the budget still group **all** files — overflow files' hunks are trimmed to their leading hunks (or omitted entirely) in the model input; the rendered page always shows the full diff.
- Server binds 127.0.0.1 only.
- Small pieces are copied from the internal `code-review-agent` repo (marked with provenance headers) pending same-source convergence.
