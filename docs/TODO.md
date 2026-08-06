# TODO

## Mechanical-cohort flag (pre-collapsed rendering)

Deferred — judged minimum benefit for now (cohort *ordering* already pushes
mechanical changes last).

Problem: the page gives no per-cohort attention cue — a "tests + lockfile"
cohort opens fully expanded just like the core-logic cohort, so the reviewer's
default scroll path doesn't match the intended attention allocation.

Sketch, if it ever earns its keep:

- `Cohort` gains `Mechanical bool` (`json:"mechanical"`); one prompt rule: set
  it for cohorts that are wholly tests, generated files, lockfiles, or
  formatting.
- `page.tmpl`: render flagged cohorts with the existing `collapsed` class and
  a small "mechanical" tag in the header.
- `repairGrouping` passes the flag through untouched; absent → `false` →
  expanded, i.e. exactly today's behavior. Degrades cleanly by construction.
- Tests: flag survives repair; pageData round-trip carries it.

## Hunk-level cohorts

Implemented (2026-08-06) — design rationale in `hunk-level-cohorts-plan.md`.
