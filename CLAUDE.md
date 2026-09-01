# cohort-review

Local read-only CLI: Bitbucket PR URL → one LLM grouping call → cohort-grouped diff2html page on localhost. See README.md for usage.

## Hard constraints

- **Copied-code parity**: files with a `// Copied from code-review-agent …` header are intentional verbatim copies. Do not refactor, "modernize", or apply lint suggestions to them (e.g. SplitSeq) — parity enables a later same-source convergence diff.
- **Zero third-party Go deps**: stdlib only. diff2html is vendored in `assets/` (version + source URLs in `assets/VERSION`); update by re-downloading the dist files, never via npm at runtime.
- **The page always renders**: grouping failures (LLM error, bad JSON) must degrade — retry once, then a single "All changes" cohort. Never exit or serve an error page because of the LLM. Folding (`fold.go`) compresses the MODEL INPUT only — DELETED/SIMILAR summary lines replace folded hunks, but every file stays in the changed-files list and the model still groups it. Repair rules in `cohorts.go` guarantee every hunk of every changed file lands in exactly one cohort (semantic-misc cohorts float first; the untyped "Unclassified (grouping fallback)" bucket stays last; whole files are the common case; mixed-concern files may be split by hunk); keep that invariant — `TestRepairHunkPartition` pins it, and the page's whole-at-first-ref fallback is its JS half.
- Server binds 127.0.0.1 only; diffs may be proprietary.
- **Metrics never block**: `--metrics` (default `~/.cohort-review/runs.jsonl`) appends a JSONL record per load attempt, including failed and degraded runs; a write failure never blocks the page (the full record's failure logs; the error line's is dropped silently). Full records carry `cohorts`/`anchor` — filter on those before averaging `misc_lines_pct`.

## Testing

`go test ./...` — pure parts only. The LLM is injected as `groupFunc` (canned outputs in tests); never call claude or the network in tests. `page_script_test.go` shells `node --check` over the page's inline JS and skips when node is absent. E2E needs real creds (`BITBUCKET_EMAIL`/`BITBUCKET_API_TOKEN`) + a logged-in `claude` CLI: run against a real PR URL and check every file appears in exactly one cohort (a split file's hunks must together appear exactly once across cohorts).
