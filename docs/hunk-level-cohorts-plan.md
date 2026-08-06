# Hunk-level cohorts — implementation plan

**Status: implemented (2026-08-06).** Kept for design rationale. The invariant
lives in CLAUDE.md; `TestRepairHunkPartition` pins the Go half and the page's
whole-at-first-ref fallback is the JS half.

## Problem and goal

Today every file lands in exactly one cohort, but the unit of a *concern* is
the hunk: a mixed-concern file drags foreign hunks into whichever cohort it
lands in. Goal: allow a file's hunks to be split across cohorts. The core
invariant generalizes from "every changed file in exactly one cohort" to
**"every hunk of every changed file in exactly one cohort"**, with whole-file
membership remaining the common case. Hard constraints preserved: never-fail
degrade (page always renders, full diff exactly once), prompt-injection
fencing, stdlib only, parity-copied files untouched.

## Design decisions

**Addressing — `path#N` microformat.** `Cohort.Files` entries stay strings; a
split ref is `path#N` (N = 1-based hunk index in that file's diff order), a
bare `path` claims all still-unclaimed hunks. No ranges — the model emits one
entry per hunk. Because `#` is legal in filenames, resolution tries the whole
entry as a bare path first (exact-match-wins, like `resolvePath`), then splits
at the *last* `#`; trim whitespace/backticks once before both attempts. A repo
containing both literal `util.go#2` and `util.go` can never hunk-address
util.go's hunk 2 — acceptable, degrades to whole-file.

**Ground truth.** Per-path hunk counts from `splitSegmentHunks` over the
segments (`indexSegments(segs) diffIndex` with `paths []string` in diff order
and `hunks map[string]int`). Count 0 means bare-only: hunkless segments
(binary, mode-only) AND paths appearing in more than one segment (see
"pre-existing bug" below).

**Repair semantics.** Walk cohorts and entries in order:
- `path#N` claims hunk N; invalid path or index → drop entry, log (same style
  as hallucinated paths).
- Bare `path` claims all unclaimed hunks; already-claimed → silent skip
  (mirrors today's duplicate rule: first cohort wins). Bare after `#N`
  elsewhere therefore means "the rest of the file" — order-dependent by
  design.
- Per cohort, merge one path's claims into a single ref at its first
  occurrence, hunks sorted ascending. Claims covering all M hunks normalize to
  a whole-file ref.
- After the walk, unclaimed hunks per path → trailing "Other" (whole-file ref
  if the leftover set is the full set).
- Empty cohorts pruned; single-cohort fallback unchanged (bare refs).

**Type split — model-facing vs page-facing (firm decision).** The LLM parse
type keeps `Files []string`; repair outputs a normalized page-facing
`FileRef{Path string; Hunks []int}` (nil = whole file; else sorted, unique,
validated). The page must stay a dumb renderer of pre-validated data —
shipping microformat to JS would duplicate the entire tolerant-parse/validate
apparatus in a second language, where divergence silently breaks
render-exactly-once.

**Prompt annotation.** The model must see hunk numbering: a column-0 line
`### hunk path#N` immediately before each `@@` line, injected at prompt-build
time for every included file (uniformly, even single-hunk files — ~20 bytes
each). Spoofing is bounded: every line inside fenced hunk content starts with
`+`/`-`/space/`\`/header text, so an attacker can at best produce a visibly
added `+### hunk …` line; a column-0 annotation is always ours, and repair
validates all indices anyway (worst case misgrouping, never breakage).
**Trap:** never feed annotated text back through `splitSegmentHunks`/
`trimSegment` — annotations precede the `@@` line and would glue onto the
previous chunk. Order is always: trim raw, then annotate. Annotated bytes
count against the budget.

**Prompt rules.** Split only when hunks clearly serve different concerns;
prefer whole files; one entry per hunk, bare = remaining; if you split a file,
account for every one of its hunks; **never** use `#N` on files whose hunks
are not shown (Omitted) or only partially shown (Truncated) — a blind claim
validates but splits a file on zero evidence, and a half-seen split dumps the
unseen hunks into "Other".

**Rendering.** Verified against the vendored diff2html 3.4.56: hunk line
numbers derive solely from each `@@` header, so file header + any subset of
hunks renders correctly. JS mirrors the Go split (`block.split(/(?=^@@ )/m)`
— identical boundaries since only real hunk headers can start a line with
`"@@ "` inside a segment). Sidebar shows a `k/M hunks` badge for partial refs
(M = parts.length − 1, computed client-side). Anchors `f-${ci}-${fi}` are
index-based and survive one path appearing in multiple cohorts. Top meta
counts unique paths, not wrappers.

**The JS never-fail fallback (not optional).** If a ref's index is ever out of
range of the JS split (any future Go/JS asymmetry), filtering indices would
*silently drop hunks from the page* — the one degradation rendering is never
allowed. Instead mark the path unsplittable: render the whole block at the
path's **first** ref, nothing at later refs (`jumpFile` already falls back to
the cohort header for missing anchors). Any mirror failure then degrades
grouping, never rendering.

**Pre-existing bug to fix in passing.** `blockByPath[path] = block` in
page.tmpl *overwrites*, so a diff with two segments for one path already
renders only the last block. Fix: JS appends and marks the path unsplittable;
Go forces `hunkCount = 0` for duplicated paths (bare-only).

## Phases

### Phase 1 — Go plumbing (behavior identical for bare paths)

In `cohorts.go`: `FileRef`, model-facing parse types, `indexSegments`,
`resolveRef(f, idx, valid) (path, hunk, ok)`, generalized
`repairGrouping(g, idx) Grouping`. `groupCohorts` builds the index once;
fallback emits bare refs. Mechanical `FileRef` updates in `server_test.go`
fixtures.

Tests: `TestIndexSegments` (counts from sampleDiff, binary → 0, duplicate path
→ 0); `TestResolveRef` (bare exact, valid `#N`, invalid `#0`/`#99`/on-binary/
non-numeric, literal-`#` filename exact-match wins, backtick/prefix/tab
tolerance); repair suite (split across cohorts, bare-claims-remainder,
first-wins, same-cohort merge+sort+normalize, leftovers→Other, invalid ref
dropped); **`TestRepairHunkPartition`** — the load-bearing invariant test:
over several canned outputs, every (path, hunk) appears exactly once across
cohorts ∪ Other. Gate: all existing repair/fallback/order tests pass with only
type-shape edits.

### Phase 2 — Prompt

`annotateSegmentHunks(path, raw)` (hunkless → unchanged); `buildPromptInput`
annotates whole and post-trim truncated segments, counts annotated bytes;
`buildCohortPrompt` softens "EXACTLY ONE cohort" with the split exception and
adds the split rule block. Tests: annotation reconstruction (strip
`^### hunk ` lines → raw), truncated files annotated 1..k only, rules
presence; update the exact-budget arithmetic in the two truncation tests.

### Phase 3 — Page

`fileBlocks: path → {block, parts}` (append + unsplittable on duplicates);
`subFor(ref)` with the whole-at-first-ref fallback; per-cohort `subs` computed
once so anchor index pairing stays exact; nav badges; unique-path meta count.
Manual browser verification: both formats, dark mode, collapse, `/load` swap.

### Phase 4 — E2E

Real PRs: one genuine mixed-concern file, one binary + over-budget lockfile
(exercises bare-only + Omitted), one rename-plus-behavior. Exactly-once audit
from the console: per path, rendered `@@` count across cohorts must equal
`parts.length − 1`. Update CLAUDE.md's invariant sentence ("every changed
file" → "every hunk of every changed file") and this doc's status.

## Sizing

Phase 1 ~180 lines Go + ~300 test (the repair rewrite is the bulk, ~half a
day); Phase 2 ~50 lines (~2h); Phase 3 ~70 lines JS/CSS + manual verification
(~2–3h); Phase 4 ~1h per PR. Roughly 1.5–2 focused days.
