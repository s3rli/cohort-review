# cohort-review

Local read-only CLI: Bitbucket PR URL → one LLM grouping call → cohort-grouped diff2html page on localhost. See README.md for usage.

## Hard constraints

- **Copied-code parity**: files with a `// Copied from code-review-agent …` header are intentional verbatim copies. Do not refactor, "modernize", or apply lint suggestions to them (e.g. SplitSeq) — parity enables a later same-source convergence diff.
- **Zero third-party Go deps**: stdlib only. diff2html is vendored in `assets/` (version + source URLs in `assets/VERSION`); update by re-downloading the dist files, never via npm at runtime.
- **The page always renders**: grouping failures (LLM error, bad JSON) must degrade — retry once, then a single "All changes" cohort. Never exit or serve an error page because of the LLM. Repair rules in `cohorts.go` guarantee every hunk of every changed file lands in exactly one cohort (whole files are the common case; mixed-concern files may be split by hunk); keep that invariant — `TestRepairHunkPartition` pins it, and the page's whole-at-first-ref fallback is its JS half.
- Server binds 127.0.0.1 only; diffs may be proprietary.

## Testing

`go test ./...` — pure parts only. The LLM is injected as `groupFunc` (canned outputs in tests); never call claude or the network in tests. E2E needs real creds (`BITBUCKET_EMAIL`/`BITBUCKET_API_TOKEN`) + a logged-in `claude` CLI: run against a real PR URL and check every file appears in exactly one cohort (a split file's hunks must together appear exactly once across cohorts).
