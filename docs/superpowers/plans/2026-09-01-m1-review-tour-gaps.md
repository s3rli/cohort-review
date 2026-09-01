# M1 — Review-Tour Gaps (fold / claims / misc flip / JSONL metrics) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring cohort-review up to the M1 level of the PR 導讀 spec, following the M1 handoff (artifact `b499563a…` — where it conflicts with the spec, **the handoff wins**): W1 pre-LLM mechanical folding, W2 claim + type taxonomy, W3 misc semantic flip, W4 JSONL metrics, W6 golden eval harness, W5 anchors (optional).

**Architecture:** Folding is a **prompt-input compression, not a grouping change**: deletion groups (status-D files by top-level dir, ≥5) and same-shape groups (dir × ext × churn magnitude, ≥5) replace their hunks with one summary line each (`### DELETED: …` listing all paths / `### SIMILAR (like rep): …` keeping the representative's hunks), and prompt rules tell the **model** to group them into `deletion` / `mechanical` cohorts. The changed-files list, repair pipeline, and partition invariant are untouched. `Cohort` keeps `Summary` and gains `Claim` (question form) + `Type` (`claim`/`mechanical`/`deletion`/`nonfix`/`misc`; unknown → `claim`). Semantic misc cohorts float first; the repair leftovers bucket is renamed "Unclassified (grouping fallback)" and stays last — two different things, kept separate. Every run (including failed/degraded) appends one line to `~/.cohort-review/runs.jsonl`.

**Tech Stack:** Go stdlib only (hard constraint), vendored diff2html, `claude` CLI injected as `groupFunc` (never called in unit tests; the tag-gated golden harness is the one deliberate exception, run manually via `make golden`).

**Invariants that must survive every task** (CLAUDE.md + handoff):
- Every hunk of every changed file lands in exactly one cohort — repair is untouched, so `TestRepairHunkPartition` stays green **unmodified**.
- LLM failure degrades to a single "All changes" cohort; the page always renders the **full** diff (folding affects only model input and grouping).
- Nonce fences stay; 127.0.0.1 only; read-only — never POST to the PR.
- Never modify `// Copied from code-review-agent` functions; keep the convergence comments. (`deriveNameStatus` is new-here and fair game.)
- **Do not touch** the `cohort-review--wt-lazy-diff` worktree.
- Commit style: conventional commits, no AI attribution or trailers of any kind.

**Out of M1 scope:** CRA integration, review-thread anchors, PR-size trigger conditions, stacked-PR tooling, tier-6 untangling. (W5 anchors are in scope but optional — last task.)

---

## Task 0: Branch

- [ ] **Step 1: Create the feature branch** (name per handoff)

```bash
cd /Users/thomasli/Documents/workspace/cohort-review
git checkout -b feature/spec-v1-gaps
go vet ./... && go test ./...   # baseline: expect all PASS
```

---

## Task 1: Extract `segmentStat` from `deriveNameStatus`

Folding needs per-segment status + churn; `deriveNameStatus` already computes it inline. Extract it (new-here code, no parity violation).

**Files:**
- Modify: `diffsplit.go:87-130` (`deriveNameStatus`)
- Test: `diffsplit_test.go`

- [ ] **Step 1: Write the failing test**

Append to `diffsplit_test.go`:

```go
func TestSegmentStat(t *testing.T) {
	cases := []struct {
		raw     string
		status  string
		added   int
		deleted int
	}{
		{"diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-x\n+y\n", "M", 1, 1},
		{"diff --git a/n.go b/n.go\nnew file mode 100644\n+++ b/n.go\n@@ -0,0 +1,2 @@\n+a\n+b\n", "A", 2, 0},
		{"diff --git a/d.go b/d.go\ndeleted file mode 100644\n--- a/d.go\n@@ -1,2 +0,0 @@\n-a\n-b\n", "D", 0, 2},
		{"diff --git a/o.go b/r.go\nrename from o.go\nrename to r.go\n", "R", 0, 0},
	}
	for _, c := range cases {
		status, added, deleted := segmentStat(FileSegment{Path: "p", Raw: c.raw})
		if status != c.status || added != c.added || deleted != c.deleted {
			t.Errorf("segmentStat(%.30q) = %s +%d/-%d, want %s +%d/-%d",
				c.raw, status, added, deleted, c.status, c.added, c.deleted)
		}
	}
}
```

- [ ] **Step 2: Run it — expect FAIL (undefined: segmentStat)**

```bash
go test ./... -run TestSegmentStat
```

- [ ] **Step 3: Implement — split the function**

In `diffsplit.go`, replace the body of `deriveNameStatus` (per-line logic moves verbatim into the new function):

```go
// segmentStat derives one segment's name-status fields (status letter,
// added/deleted line counts) from its raw text; shared by deriveNameStatus
// and mechanical folding.
func segmentStat(s FileSegment) (status string, added, deleted int) {
	status = "M"
	inHunk := false
	for _, line := range strings.Split(s.Raw, "\n") {
		if strings.HasPrefix(line, "@@ ") {
			inHunk = true
			continue
		}
		if !inHunk {
			switch {
			case strings.HasPrefix(line, "new file mode"):
				status = "A"
			case strings.HasPrefix(line, "deleted file mode"):
				status = "D"
			case strings.HasPrefix(line, "rename from "):
				status = "R"
			}
			continue
		}
		// The +++/--- guard matters only for diff-of-a-diff content,
		// where an added line can itself be a file header.
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			added++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			deleted++
		}
	}
	return status, added, deleted
}

func deriveNameStatus(segs []FileSegment) string {
	var b strings.Builder
	for _, s := range segs {
		if s.Path == "" {
			continue
		}
		status, added, deleted := segmentStat(s)
		fmt.Fprintf(&b, "%s\t%s\t+%d/-%d\n", status, s.Path, added, deleted)
	}
	return b.String()
}
```

- [ ] **Step 4: Run the full suite — expect PASS**

```bash
go test ./...
```

- [ ] **Step 5: Commit**

```bash
git add diffsplit.go diffsplit_test.go
git commit -m "refactor(diff): extract segmentStat from deriveNameStatus"
```

---

## Task 2: W2 fields — `Claim` + `Type` on Cohort (Summary stays)

Handoff W2.1/W2.2: add two fields; repair is tolerant (missing/unknown type → `claim`); prompt asks for a **question-form** claim, the deletion three-question claim, the mechanical representative claim, and the nonfix shape rule; misc rule per W3.1.

**Files:**
- Modify: `cohorts.go` (Cohort, modelCohort, prompt rules, repairGrouping, header comment)
- Test: `cohorts_test.go`

- [ ] **Step 1: Write the failing tests**

In `cohorts_test.go`, replace the `goodJSON` const with:

```go
const goodJSON = `{"walkthrough": "Adds auth.", "cohorts": [
	{"name": "Auth", "summary": "core auth", "claim": "Does auth validate sessions?", "type": "claim", "files": ["core/auth.go", "core/session.go"]},
	{"name": "Docs", "summary": "docs", "claim": "Do the docs match the new flow?", "type": "claim", "files": ["docs/README.md"]}]}`
```

Append:

```go
func TestTypeAndClaimParsedValidated(t *testing.T) {
	out := `{"cohorts": [
		{"name": "A", "summary": "s", "claim": "is the decision sound?", "type": "nonfix", "files": ["core/auth.go"]},
		{"name": "B", "summary": "s", "claim": "b?", "type": "bogus", "files": ["core/session.go", "docs/README.md"]}]}`
	g := groupCohorts(context.Background(), canned(out), PullRequest{}, testSegs(), defaultBudget)
	if g.Cohorts[0].Claim != "is the decision sound?" || g.Cohorts[0].Type != "nonfix" {
		t.Errorf("cohort A: %+v", g.Cohorts[0])
	}
	if g.Cohorts[0].Summary != "s" {
		t.Errorf("summary lost: %+v", g.Cohorts[0])
	}
	if g.Cohorts[1].Type != "claim" {
		t.Errorf("bogus type not normalized to claim: %+v", g.Cohorts[1])
	}
}

func TestClaimTypeRulesInPrompt(t *testing.T) {
	p := buildCohortPrompt(promptInput{NameStatus: "M\ta.go\t+1/-0\n"}, "nonce")
	for _, frag := range []string{`"claim"`, `"type"`, "nonfix", "transition-period risk",
		"never force-fit", "review signal"} {
		if !strings.Contains(p, frag) {
			t.Errorf("prompt missing rule fragment %q", frag)
		}
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./... -run 'TestTypeAndClaim|TestClaimTypeRules'
```

- [ ] **Step 3: Implement the model change in `cohorts.go`**

Replace the `Cohort` and `modelCohort` types:

```go
type Cohort struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	// Claim is the question the reviewer must answer to approve this cohort
	// (question form — 三次盲測定案). Deletion cohorts carry the fixed three
	// questions. Empty on untyped fallback cohorts.
	Claim string `json:"claim"`
	// Type: "claim" | "mechanical" | "deletion" | "nonfix" | "misc".
	// Empty means untyped (repair fallback bucket, degraded cohort); unknown
	// model values normalize to "claim" in repair.
	Type  string    `json:"type"`
	Files []FileRef `json:"files"`
}
```

```go
type modelCohort struct {
	Name    string   `json:"name"`
	Summary string   `json:"summary"`
	Claim   string   `json:"claim"`
	Type    string   `json:"type"`
	Files   []string `json:"files"`
}
```

Add next to `repairGrouping`:

```go
// validType normalizes a model-emitted cohort type; anything unexpected is a
// plain claim cohort (handoff W2: missing → claim, unknown → claim).
func validType(t string) string {
	switch t {
	case "claim", "mechanical", "deletion", "nonfix", "misc":
		return t
	}
	return "claim"
}
```

In `repairGrouping`, change the kept-cohort construction line to:

```go
		kept = append(kept, Cohort{Name: mc.Name, Summary: mc.Summary,
			Claim: mc.Claim, Type: validType(mc.Type), Files: refs})
```

(The trailing "Other" cohort and the "All changes" fallback stay untouched here — their zero-value `Type: ""` is the untyped state; Task 6 renames the bucket.)

In `buildCohortPrompt`, replace the JSON-shape line with:

```go
	b.WriteString(`{"walkthrough": "...", "cohorts": [{"name": "...", "summary": "...", "claim": "...", "type": "claim", "files": ["path", ...]}]}` + "\n\n")
```

and directly after the existing `- "name": a short title (2-5 words). "summary": …` rule, add:

```go
	b.WriteString("- \"claim\": ONE question the reviewer must answer to approve the cohort (e.g. \"Does endpoint X now match the old gateway's behavior?\"). Ground it in what the hunks actually change.\n")
	b.WriteString("- \"type\": \"claim\" for a normal verifiable intent (the default). \"mechanical\" for same-shape repeated churn, generated files, lockfiles, formatting, mass renames, or mechanical test churn — its claim is \"Verify the representative <path>; are the rest really the same shape?\". \"deletion\" for cohorts of deleted files — its claim is exactly: \"Is the removal complete? What takes over the coverage these files provided? What is the transition-period risk?\". \"nonfix\" when the cohort documents and pins CURRENT behavior instead of changing it (docs plus tests asserting the status quo) — the reviewer reviews the decision not to fix, not the code.\n")
	b.WriteString("- Hunks that match NO intent stated in the PR title/description and do not clearly belong with any other cohort go in ONE cohort with \"type\": \"misc\", its summary noting these changes are undeclared and the author should be asked — never force-fit them into an intent cohort. Undeclared changes are a review signal.\n")
```

- [ ] **Step 4: Update the `cohorts.go` header comment**

Extend the "Deliberate divergences" list in the package comment with: `claim+type cohort fields (question-form claims, five-type taxonomy), prompt-side DELETED/SIMILAR folding, and a semantic-misc-first ordering`.

- [ ] **Step 5: Run the full suite — expect PASS**

```bash
go test ./...
```

- [ ] **Step 6: Commit**

```bash
git add cohorts.go cohorts_test.go
git commit -m "feat(cohorts): claim and type fields with question-form claim rules"
```

---

## Task 3: W1 deletion folds (`fold.go`)

Handoff W1.1: pure-deletion segments aggregate **by directory**; their hunks are not sent — a summary line replaces them (Task 5 does the prompt part; this task computes the folds).

**Files:**
- Create: `fold.go`
- Create: `fold_test.go`

- [ ] **Step 1: Write the failing tests**

Create `fold_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

// delSeg builds a deleted-file segment with 2 removed lines.
func delSeg(p string) FileSegment {
	return seg(p, "diff --git a/"+p+" b/"+p+"\ndeleted file mode 100644\n--- a/"+p+
		"\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-x\n-y\n")
}

// mechSeg builds a modified-file segment with `pairs` -/+ line pairs
// (churn = 2*pairs).
func mechSeg(p string, pairs int) FileSegment {
	return seg(p, "diff --git a/"+p+" b/"+p+"\n--- a/"+p+"\n+++ b/"+p+"\n@@ -1 +1 @@\n"+
		strings.Repeat("-x\n+y\n", pairs))
}

func fiveDels() []FileSegment {
	return []FileSegment{delSeg("old/a.go"), delSeg("old/b.go"), delSeg("old/c.go"),
		delSeg("old/d.go"), delSeg("old/e.go")}
}

func TestFoldDeletionsByDir(t *testing.T) {
	segs := append(testSegs(), fiveDels()...)
	segs = append(segs, delSeg("legacy/x.go")) // lone deletion in another dir: below threshold
	folds := foldSegments(segs)
	if len(folds) != 1 || folds[0].Kind != "deletion" {
		t.Fatalf("folds = %+v", folds)
	}
	f := folds[0]
	if len(f.Members) != 5 || f.Dir != "old" || f.Lines != 10 || f.Members[0] != "old/a.go" {
		t.Errorf("deletion fold = %+v", f)
	}
}

func TestFoldDeletionsBelowThreshold(t *testing.T) {
	segs := append(testSegs(),
		delSeg("old/a.go"), delSeg("old/b.go"), delSeg("old/c.go"), delSeg("old/d.go"))
	if folds := foldSegments(segs); len(folds) != 0 {
		t.Errorf("4 deletions must not fold: %+v", folds)
	}
}

func TestFoldSkipsDuplicatePaths(t *testing.T) {
	// A duplicated path is bare-only in the hunk index and must stay visible
	// to the model — with the dup excluded only 4 deletions remain: no fold.
	segs := []FileSegment{
		delSeg("old/a.go"), delSeg("old/a.go"),
		delSeg("old/b.go"), delSeg("old/c.go"), delSeg("old/d.go"), delSeg("old/e.go"),
	}
	if folds := foldSegments(segs); len(folds) != 0 {
		t.Errorf("duplicate path folded: %+v", folds)
	}
}

func TestTopDir(t *testing.T) {
	for p, want := range map[string]string{"a/b/c.go": "a", "root.go": "."} {
		if got := topDir(p); got != want {
			t.Errorf("topDir(%q) = %q, want %q", p, got, want)
		}
	}
}
```

- [ ] **Step 2: Run — expect FAIL (undefined: foldSegments, topDir)**

```bash
go test ./... -run 'TestFold|TestTopDir'
```

- [ ] **Step 3: Implement `fold.go` (deletion folds only; similar folds arrive in Task 4)**

```go
// Mechanical folding (spec S2 / handoff W1): deterministic, diffstat-only
// compression of the MODEL INPUT, run before the LLM call. Folded files stay
// in the changed-files list and are still grouped by the model — only their
// hunks are replaced with a summary line, and prompt rules direct them into
// deletion/mechanical cohorts. Grouping ground truth and repair are untouched.
package main

import "strings"

// minFoldSize is deliberately conservative: a false fold hides real hunks from
// the model, and folding only pays off on genuine repetition.
const minFoldSize = 5

// A Fold is one planned summary line: files whose hunks the model won't see.
type Fold struct {
	Kind    string   // "deletion" | "similar"
	Members []string // paths whose hunks are replaced, diff order
	Rep     string   // similar folds: representative whose hunks ARE still sent
	Dir     string   // deletion folds: shared top-level directory label
	Lines   int      // churn folded out of the prompt (members only)
}

func topDir(p string) string {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return "."
}

// foldSegments plans prompt-side folding. Preamble (empty Path) and duplicate
// paths are never folded: duplicates are bare-only in the hunk index and must
// stay visible to the model.
func foldSegments(segs []FileSegment) []Fold {
	type stat struct {
		status         string
		added, deleted int
	}
	stats := make(map[string]stat, len(segs))
	seen := make(map[string]int, len(segs))
	for _, s := range segs {
		if s.Path == "" {
			continue
		}
		seen[s.Path]++
		st, a, d := segmentStat(s)
		stats[s.Path] = stat{st, a, d}
	}
	foldable := func(p string) bool { return p != "" && seen[p] == 1 }

	// Deletion folds: status-D files grouped by top-level directory.
	delGroups := make(map[string][]string)
	var delOrder []string
	for _, s := range segs {
		if !foldable(s.Path) || stats[s.Path].status != "D" {
			continue
		}
		d := topDir(s.Path)
		if len(delGroups[d]) == 0 {
			delOrder = append(delOrder, d)
		}
		delGroups[d] = append(delGroups[d], s.Path)
	}
	var folds []Fold
	for _, d := range delOrder {
		members := delGroups[d]
		if len(members) < minFoldSize {
			continue
		}
		f := Fold{Kind: "deletion", Members: members, Dir: d}
		for _, p := range members {
			f.Lines += stats[p].added + stats[p].deleted
		}
		folds = append(folds, f)
	}
	return folds
}
```

- [ ] **Step 4: Run the full suite — expect PASS**

```bash
go test ./...
```

- [ ] **Step 5: Commit**

```bash
git add fold.go fold_test.go
git commit -m "feat(fold): plan deletion folds by top-level directory"
```

---

## Task 4: W1 same-shape ("SIMILAR") folds

Handoff W1.2: same ext × same dir × similar churn magnitude (log bucket), group ≥5 → send only the representative's hunks, list the rest.

**Files:**
- Modify: `fold.go`
- Test: `fold_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `fold_test.go`:

```go
func TestFoldSimilarKeepsRepresentative(t *testing.T) {
	segs := []FileSegment{
		mechSeg("cfg/a.yaml", 1), mechSeg("cfg/b.yaml", 1), mechSeg("cfg/c.yaml", 1),
		mechSeg("cfg/d.yaml", 1), mechSeg("cfg/e.yaml", 4), // churn 8 — largest, same magnitude bucket
		mechSeg("other/x.go", 1),
	}
	folds := foldSegments(segs)
	if len(folds) != 1 || folds[0].Kind != "similar" {
		t.Fatalf("folds = %+v", folds)
	}
	f := folds[0]
	if f.Rep != "cfg/e.yaml" {
		t.Errorf("rep = %q, want cfg/e.yaml", f.Rep)
	}
	if len(f.Members) != 4 || f.Members[0] != "cfg/a.yaml" {
		t.Errorf("members must exclude the representative: %+v", f.Members)
	}
	if f.Lines != 8 { // 4 members × churn 2; the rep's 8 lines are still sent
		t.Errorf("Lines = %d, want 8", f.Lines)
	}
}

func TestFoldSimilarSplitsByMagnitude(t *testing.T) {
	// Same dir+ext but churn 2 vs 200: different buckets, neither reaches 5.
	segs := []FileSegment{
		mechSeg("cfg/a.yaml", 1), mechSeg("cfg/b.yaml", 1), mechSeg("cfg/c.yaml", 1),
		mechSeg("cfg/d.yaml", 100), mechSeg("cfg/e.yaml", 100),
	}
	if folds := foldSegments(segs); len(folds) != 0 {
		t.Errorf("magnitude buckets must not merge: %+v", folds)
	}
}

func TestFoldDeletionTakesPrecedence(t *testing.T) {
	// 5 same-shape deleted files fold as deletion, not similar.
	folds := foldSegments(fiveDels())
	if len(folds) != 1 || folds[0].Kind != "deletion" {
		t.Errorf("folds = %+v", folds)
	}
}

func TestMagnitude(t *testing.T) {
	for n, want := range map[int]int{0: 0, 9: 0, 10: 1, 99: 1, 100: 2, 15000: 4} {
		if got := magnitude(n); got != want {
			t.Errorf("magnitude(%d) = %d, want %d", n, got, want)
		}
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./... -run 'TestFoldSimilar|TestFoldDeletionTakes|TestMagnitude'
```

- [ ] **Step 3: Implement — add the similar pass to `foldSegments`**

Add `"path"` and `"strconv"` to `fold.go` imports, plus:

```go
// magnitude buckets churn by order of magnitude: 0–9 → 0, 10–99 → 1, 100–999 → 2…
// "similar change size" for the same-shape fold key.
func magnitude(n int) int {
	m := 0
	for n >= 10 {
		n /= 10
		m++
	}
	return m
}
```

In `foldSegments`, after the deletion-folds loop (before `return folds`), insert:

```go
	inFold := make(map[string]bool)
	for _, f := range folds {
		for _, p := range f.Members {
			inFold[p] = true
		}
	}
	simGroups := make(map[string][]string)
	var simOrder []string
	for _, s := range segs {
		p := s.Path
		if !foldable(p) || inFold[p] || stats[p].status == "D" {
			continue
		}
		st := stats[p]
		key := path.Dir(p) + "\x00" + path.Ext(p) + "\x00" + strconv.Itoa(magnitude(st.added+st.deleted))
		if len(simGroups[key]) == 0 {
			simOrder = append(simOrder, key)
		}
		simGroups[key] = append(simGroups[key], p)
	}
	for _, k := range simOrder {
		group := simGroups[k]
		if len(group) < minFoldSize {
			continue
		}
		f := Fold{Kind: "similar"}
		best := -1
		for _, p := range group {
			if c := stats[p].added + stats[p].deleted; c > best {
				best, f.Rep = c, p
			}
		}
		for _, p := range group {
			if p == f.Rep {
				continue
			}
			f.Members = append(f.Members, p)
			f.Lines += stats[p].added + stats[p].deleted
		}
		folds = append(folds, f)
	}
```

- [ ] **Step 4: Run the full suite — expect PASS**

```bash
go test ./...
```

- [ ] **Step 5: Commit**

```bash
git add fold.go fold_test.go
git commit -m "feat(fold): same-shape similar folds with representative"
```

---

## Task 5: W1 prompt-side folding — DELETED/SIMILAR lines in the model input

Folded members' hunks are replaced by one column-0 summary line per fold (all paths listed, per handoff); representatives keep their annotated hunks; existing greedy budget truncation still runs afterward as the backstop. Prompt rules direct DELETED paths into a `deletion` cohort and SIMILAR paths into their representative's `mechanical` cohort.

**Files:**
- Modify: `cohorts.go` (`buildPromptInput` signature + body, `buildCohortPrompt` rules, `groupCohorts`, `Grouping`)
- Test: `cohorts_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cohorts_test.go`:

```go
func TestFoldedPromptDeletion(t *testing.T) {
	segs := append(testSegs(), fiveDels()...)
	in := buildPromptInput(segs, foldSegments(segs), defaultBudget)
	if !strings.Contains(in.Hunks, "### DELETED: 5 files under old/ (-10 lines): old/a.go, old/b.go, old/c.go, old/d.go, old/e.go") {
		t.Errorf("DELETED line missing or wrong: %q", in.Hunks)
	}
	if strings.Contains(in.Hunks, "diff --git a/old/a.go") {
		t.Error("folded hunks still in prompt")
	}
	if len(in.Omitted) != 0 {
		t.Errorf("folded files wrongly listed as omitted: %v", in.Omitted)
	}
	// every file must remain groupable: changed-files list intact
	for _, p := range []string{"old/a.go", "core/auth.go"} {
		if !strings.Contains(in.NameStatus, p) {
			t.Errorf("changed-files list missing %s", p)
		}
	}
}

func TestFoldedPromptSimilarKeepsRepHunks(t *testing.T) {
	segs := []FileSegment{
		mechSeg("cfg/a.yaml", 1), mechSeg("cfg/b.yaml", 1), mechSeg("cfg/c.yaml", 1),
		mechSeg("cfg/d.yaml", 1), mechSeg("cfg/e.yaml", 4),
	}
	in := buildPromptInput(segs, foldSegments(segs), defaultBudget)
	if !strings.Contains(in.Hunks, "### hunk cfg/e.yaml#1") {
		t.Error("representative hunks missing")
	}
	if !strings.Contains(in.Hunks, "### SIMILAR (like cfg/e.yaml): cfg/a.yaml, cfg/b.yaml, cfg/c.yaml, cfg/d.yaml") {
		t.Errorf("SIMILAR line missing or wrong: %q", in.Hunks)
	}
	if strings.Contains(in.Hunks, "### hunk cfg/a.yaml") {
		t.Error("member hunks still in prompt")
	}
}

func TestFoldRulesInPrompt(t *testing.T) {
	p := buildCohortPrompt(promptInput{NameStatus: "M\ta.go\t+1/-0\n"}, "nonce")
	for _, frag := range []string{"### DELETED:", "### SIMILAR", `"type": "deletion"`,
		`"type": "mechanical"`} {
		if !strings.Contains(p, frag) {
			t.Errorf("prompt missing fold rule fragment %q", frag)
		}
	}
}

func TestGroupCohortsFoldedLines(t *testing.T) {
	segs := append(testSegs(), fiveDels()...)
	g := groupCohorts(context.Background(), canned(goodJSON), PullRequest{}, segs, defaultBudget)
	if g.FoldedLines != 10 {
		t.Errorf("FoldedLines = %d, want 10", g.FoldedLines)
	}
	// The folded files were not claimed by goodJSON — repair still sweeps them,
	// so the partition invariant holds with folding active.
	last := g.Cohorts[len(g.Cohorts)-1]
	if len(last.Files) != 5 {
		t.Errorf("folded files not swept by repair: %+v", last)
	}
}
```

- [ ] **Step 2: Run — expect FAIL (buildPromptInput signature; missing lines/rules)**

```bash
go test ./... -run 'TestFoldedPrompt|TestFoldRules|TestGroupCohortsFoldedLines'
```

- [ ] **Step 3: Implement in `cohorts.go`**

Add to `fold.go` (it renders fold state):

```go
// foldLine renders one fold as a column-0 marker line inside the diff fence —
// like "### hunk", a column-0 line there is always tool-generated. All member
// paths are listed so the model can copy them verbatim into cohorts.
func foldLine(f Fold) string {
	switch f.Kind {
	case "deletion":
		return fmt.Sprintf("### DELETED: %d files under %s/ (-%d lines): %s\n",
			len(f.Members), f.Dir, f.Lines, strings.Join(f.Members, ", "))
	default: // similar
		return fmt.Sprintf("### SIMILAR (like %s): %s\n", f.Rep, strings.Join(f.Members, ", "))
	}
}
```

(add `"fmt"` to `fold.go` imports.)

Change `buildPromptInput` to take folds — signature and the loop body:

```go
// buildPromptInput assembles the model input from per-file segments. Fold
// members' hunks are replaced by one summary line per fold (emitted at the
// first member's diff position); everything else is included greedily in diff
// order as before — a segment that would overflow the budget is reduced to its
// leading whole hunks under a per-file cap, or skipped whole. Folding runs
// FIRST, the greedy budget is the backstop. All lists are prompt-only: the
// served page always renders the full diff.
func buildPromptInput(segs []FileSegment, folds []Fold, budget int) promptInput {
	in := promptInput{NameStatus: deriveNameStatus(segs)}
	skip := make(map[string]bool)
	foldAt := make(map[string]Fold) // first member's path → its fold
	for _, f := range folds {
		for _, p := range f.Members {
			skip[p] = true
		}
		foldAt[f.Members[0]] = f
	}
	perFileCap := budget / 8
	var hunks strings.Builder
	used := 0
	for _, s := range segs {
		if s.Path == "" {
			continue
		}
		if skip[s.Path] {
			f, first := foldAt[s.Path]
			if !first {
				continue
			}
			delete(foldAt, s.Path) // a duplicate-position path emits its fold once
			line := foldLine(f)
			if used+len(line) <= budget {
				hunks.WriteString(line)
				used += len(line)
			} else {
				// No room even for the summary: degrade the members to the
				// honest Omitted list instead of vanishing.
				in.Omitted = append(in.Omitted, f.Members...)
			}
			continue
		}
		ann := annotateSegmentHunks(s.Path, s.Raw)
		if used+len(ann) <= budget {
			hunks.WriteString(ann)
			used += len(ann)
			continue
		}
		trimmed := trimSegment(s.Raw, min(perFileCap, budget-used))
		if trimmed == "" {
			in.Omitted = append(in.Omitted, s.Path)
			continue
		}
		ta := annotateSegmentHunks(s.Path, trimmed)
		hunks.WriteString(ta)
		used += len(ta)
		in.Truncated = append(in.Truncated, s.Path)
	}
	in.Hunks = hunks.String()
	return in
}
```

(The existing comments about the per-file cap and trim-then-annotate stay in place.)

In `buildCohortPrompt`, extend the NEVER-path#N rule string to end with:

```
Files listed on "### DELETED:" or "### SIMILAR" lines are likewise bare-path only.
```

and after the misc rule (from Task 2), add:

```go
	b.WriteString("- A column-0 \"### DELETED:\" line in the diff content summarizes deleted files whose hunks are not shown: group ALL of its listed paths into ONE cohort with \"type\": \"deletion\".\n")
	b.WriteString("- A column-0 \"### SIMILAR (like <rep>):\" line lists files in the same directory with the same extension and a similar change size as the representative — their hunks are not shown: group them WITH that representative in a \"type\": \"mechanical\" cohort.\n")
```

`Grouping` gains internal fields:

```go
type Grouping struct {
	Walkthrough string   `json:"walkthrough"`
	Cohorts     []Cohort `json:"cohorts"`
	Degraded    bool     `json:"-"` // grouping fell back to "All changes"
	FoldedLines int      `json:"-"` // churn replaced by DELETED/SIMILAR lines
}
```

In `groupCohorts`, before `in := buildPromptInput(…)`:

```go
	folds := foldSegments(segs)
	foldedLines := 0
	for _, f := range folds {
		foldedLines += f.Lines
	}
```

change the call to `buildPromptInput(segs, folds, budget)`, add after `prompt := …`:

```go
	fmt.Fprintf(os.Stderr, "cohort-review: prompt hunks %d KB (%d lines folded)\n",
		len(in.Hunks)/1024, foldedLines)
```

set `g.Degraded`/`g.FoldedLines` on both return paths: on success `g.FoldedLines = foldedLines; return g`, and the fallback return becomes:

```go
	return Grouping{Degraded: true, FoldedLines: foldedLines, Cohorts: []Cohort{{
		Name:    "All changes",
		Summary: "Automatic grouping unavailable — showing all files.",
		Files:   bareRefs(idx.paths),
	}}}
```

- [ ] **Step 4: Fix the four existing `buildPromptInput` call sites in tests**

`TestBuildPromptInputUnderBudget`, `TestBuildPromptInputGreedySkip`, `TestBuildPromptInputTruncatesLeadingHunks`, `TestBuildPromptInputTruncationFallsBackToOmitted`: change `buildPromptInput(segs, N)` to `buildPromptInput(segs, nil, N)`.

- [ ] **Step 5: Run the full suite — expect PASS (including untouched `TestRepairHunkPartition`)**

```bash
go test ./...
```

- [ ] **Step 6: Commit**

```bash
git add fold.go cohorts.go cohorts_test.go
git commit -m "feat(cohorts): replace folded hunks with DELETED/SIMILAR prompt lines"
```

---

## Task 6: W3 misc semantic flip — two distinct buckets

Semantic misc (model-declared, `type: "misc"`) floats to the very front; the repair leftovers bucket is a **different thing** — renamed "Unclassified (grouping fallback)", untyped, stays last.

**Files:**
- Modify: `cohorts.go` (`repairGrouping` tail)
- Test: `cohorts_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `cohorts_test.go`:

```go
func TestSemanticMiscFloatsFirst(t *testing.T) {
	out := `{"cohorts": [
		{"name": "A", "summary": "s", "claim": "q?", "type": "claim", "files": ["core/auth.go", "core/util.go#1"]},
		{"name": "Odd", "summary": "undeclared refactor", "claim": "what is this for?", "type": "misc", "files": ["core/util.go#2"]}]}`
	g := groupSplit(t, out)
	if len(g.Cohorts) != 3 {
		t.Fatalf("got %+v", g.Cohorts)
	}
	first := g.Cohorts[0]
	if first.Type != "misc" || first.Name != "Odd" || first.Summary != "undeclared refactor" {
		t.Fatalf("semantic misc not floated first intact: %+v", first)
	}
	assertRefs(t, first, "core/util.go#2")
	last := g.Cohorts[2]
	if last.Name != "Unclassified (grouping fallback)" || last.Type != "" {
		t.Fatalf("repair bucket wrong: %+v", last)
	}
	assertRefs(t, last, "img.png")
}
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./... -run TestSemanticMiscFloatsFirst
```

- [ ] **Step 3: Implement — replace the tail of `repairGrouping`**

Replace the block that appends the "Other" cohort and returns, with:

```go
	// W3 semantic flip: cohorts the model marked misc are a review signal
	// ("undeclared by the description") and float to the very front. The
	// repair sweep below is a DIFFERENT thing — a fallback for files the
	// model failed to mention — and stays last, untyped.
	var misc, rest []Cohort
	for _, c := range kept {
		if c.Type == "misc" {
			misc = append(misc, c)
		} else {
			rest = append(rest, c)
		}
	}
	kept = append(misc, rest...)
	if len(missed) > 0 {
		kept = append(kept, Cohort{
			Name:    "Unclassified (grouping fallback)",
			Summary: "Files the grouping pass didn't classify.",
			Files:   missed,
		})
	}
	return Grouping{Walkthrough: g.Walkthrough, Cohorts: kept}
```

- [ ] **Step 4: Update the two name assertions on the old bucket**

- `TestRepairHallucinatedDroppedMissedToOther`: rename to `TestRepairMissedSweptToUnclassified`; change `last.Name != "Other"` to `last.Name != "Unclassified (grouping fallback)"`. (It stays last — indices unchanged.)
- `TestRepairLeftoverHunksToOther`: rename to `TestRepairLeftoverHunksToUnclassified`; same name change on the `last.Name` check.
- `TestRepairSplitAcrossCohorts`: update the trailing comment `// unclaimed binary swept to Other` → `// unclaimed binary swept to the fallback bucket`. Assertions unchanged.

All other repair tests emit fully-covering or untyped cohorts and are position-stable.

- [ ] **Step 5: Run the full suite — expect PASS**

```bash
go test ./...
```

- [ ] **Step 6: Commit**

```bash
git add cohorts.go cohorts_test.go
git commit -m "feat(cohorts): semantic misc floats first, repair bucket renamed and kept last"
```

---

## Task 7: W4 JSONL metrics — `~/.cohort-review/runs.jsonl`

Handoff schema, one line per run, **including failed/degraded runs** (`"error"` field). `misc_lines_pct` counts semantic misc only — it is the tool's quality gauge.

**Files:**
- Create: `metrics.go`
- Create: `metrics_test.go`
- Modify: `server.go` (`app`, `load`), `main.go` (flag)
- Test: `server_test.go`

- [ ] **Step 1: Write the failing tests**

Create `metrics_test.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComputeRecord(t *testing.T) {
	// splitSegs churn: auth 3 hunks × 2 lines, util 2 × 2, img 0 → total 10.
	segs := splitSegs()
	g := Grouping{FoldedLines: 4, Cohorts: []Cohort{
		{Name: "Odd", Type: "misc", Files: []FileRef{{Path: "core/util.go", Hunks: []int{1}}}},
		{Name: "A", Type: "claim", Files: []FileRef{{Path: "core/auth.go"}}},
		{Name: "Unclassified (grouping fallback)", Files: []FileRef{{Path: "core/util.go", Hunks: []int{2}}, {Path: "img.png"}}},
	}}
	pr := PullRequest{Title: "PLAT-2328 fix callbacks", Description: "Does:\n- adds a thing\n- removes another\n"}
	ref := PRRef{Workspace: "ws", Repo: "repo", Number: 18}
	r := computeRecord(ref, "sonnet", pr, g, segs, 23.4)
	if r.PR != "ws/repo#18" || r.Model != "sonnet" || r.GroupSecs != 23.4 {
		t.Errorf("identity: %+v", r)
	}
	if r.Lines != 10 || r.Files != 3 || r.Cohorts != 3 {
		t.Errorf("totals: %+v", r)
	}
	// misc pct: semantic misc only (util hunk 1 = 2 lines of 10), NOT the fallback bucket.
	if r.MiscLinesPct != 20 {
		t.Errorf("misc_lines_pct = %v, want 20", r.MiscLinesPct)
	}
	if r.FoldedLinesPct != 40 {
		t.Errorf("folded_lines_pct = %v, want 40", r.FoldedLinesPct)
	}
	if r.Types["misc"] != 1 || r.Types["claim"] != 1 || r.Types["unclassified"] != 1 {
		t.Errorf("types: %v", r.Types)
	}
	if r.Anchor == nil || r.Anchor.DescChars != len(pr.Description) ||
		r.Anchor.DescBullets != 2 || r.Anchor.JiraKey != "PLAT-2328" {
		t.Errorf("anchor: %+v", r.Anchor)
	}
	if r.Error != "" {
		t.Errorf("unexpected error field: %q", r.Error)
	}

	g.Degraded = true
	if d := computeRecord(ref, "sonnet", pr, g, segs, 1); d.Error == "" {
		t.Error("degraded run must carry an error field")
	}
}

func TestAppendJSONL(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "runs.jsonl")
	for i := 0; i < 2; i++ {
		if err := appendJSONL(p, map[string]int{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines: %q", len(lines), data)
	}
	var v struct {
		I int `json:"i"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &v); err != nil || v.I != 1 {
		t.Errorf("line 2 = %q (err %v)", lines[1], err)
	}
}
```

Append to `server_test.go` (add `"bytes"`, `"os"`, `"path/filepath"` to its imports):

```go
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
```

- [ ] **Step 2: Run — expect FAIL**

```bash
go test ./... -run 'TestComputeRecord|TestAppendJSONL|TestLoadWrites'
```

- [ ] **Step 3: Implement `metrics.go`**

```go
// Per-run JSONL metrics (handoff W4): one line per run — successful, degraded,
// or failed — appended to runs.jsonl. misc_lines_pct (semantic misc churn
// share) is the tool's quality gauge; anchor stats feed the M3 anchor-presence
// decision. Always best-effort: a metrics failure never blocks the page.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type anchorStats struct {
	DescChars   int    `json:"desc_chars"`
	DescBullets int    `json:"desc_bullets"`
	JiraKey     string `json:"jira_key,omitempty"`
}

type runRecord struct {
	TS             string         `json:"ts"`
	PR             string         `json:"pr"` // workspace/repo#id
	Lines          int            `json:"lines,omitempty"`
	Files          int            `json:"files,omitempty"`
	Cohorts        int            `json:"cohorts,omitempty"`
	Types          map[string]int `json:"types,omitempty"`
	MiscLinesPct   float64        `json:"misc_lines_pct"`
	FoldedLinesPct float64        `json:"folded_lines_pct"`
	Anchor         *anchorStats   `json:"anchor,omitempty"`
	Model          string         `json:"model,omitempty"`
	GroupSecs      float64        `json:"group_secs,omitempty"`
	Error          string         `json:"error,omitempty"`
}

// bulletLine spots "- x", "* x", "+ x", "1. x", "2) x" list lines — the
// cheapest proxy for "the author enumerated intents".
var bulletLine = regexp.MustCompile(`(?m)^[ \t]*(?:[-*+]|\d+[.)])[ \t]`)

var jiraKeyRe = regexp.MustCompile(`[A-Z]+-\d+`)

func prID(ref PRRef) string {
	return fmt.Sprintf("%s/%s#%d", ref.Workspace, ref.Repo, ref.Number)
}

func computeRecord(ref PRRef, model string, pr PullRequest, g Grouping, segs []FileSegment, groupSecs float64) runRecord {
	r := runRecord{
		TS:      time.Now().UTC().Format(time.RFC3339),
		PR:      prID(ref),
		Model:   model,
		Cohorts: len(g.Cohorts),
		Types:   map[string]int{},
		Anchor: &anchorStats{
			DescChars:   len(pr.Description),
			DescBullets: len(bulletLine.FindAllString(pr.Description, -1)),
			JiraKey:     jiraKeyRe.FindString(pr.Title),
		},
		GroupSecs: groupSecs,
	}
	if g.Degraded {
		r.Error = "grouping degraded to single cohort"
	}
	// pathChurn covers whole-path (bare) refs — including duplicate-path
	// segments, which are always bare-only; hunk refs count from the first
	// segment's hunks.
	pathChurn := map[string]int{}
	byPath := map[string]FileSegment{}
	for _, s := range segs {
		if s.Path == "" {
			continue
		}
		_, a, d := segmentStat(s)
		pathChurn[s.Path] += a + d
		r.Lines += a + d
		if _, ok := byPath[s.Path]; !ok {
			byPath[s.Path] = s
		}
	}
	r.Files = len(byPath)
	miscLines := 0
	for _, c := range g.Cohorts {
		k := c.Type
		if k == "" {
			k = "unclassified"
		}
		r.Types[k]++
		if c.Type != "misc" {
			continue
		}
		for _, f := range c.Files {
			if f.Hunks == nil {
				miscLines += pathChurn[f.Path]
			} else if s, ok := byPath[f.Path]; ok {
				miscLines += hunkChurn(s.Raw, f.Hunks)
			}
		}
	}
	if r.Lines > 0 {
		r.MiscLinesPct = 100 * float64(miscLines) / float64(r.Lines)
		r.FoldedLinesPct = 100 * float64(g.FoldedLines) / float64(r.Lines)
	}
	return r
}

// hunkChurn counts changed lines in the referenced hunks of one segment.
func hunkChurn(raw string, refs []int) int {
	_, hunks := splitSegmentHunks(raw)
	n := 0
	for _, h := range refs {
		if h < 1 || h > len(hunks) {
			continue
		}
		for _, line := range strings.Split(hunks[h-1], "\n") {
			if (strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++")) ||
				(strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---")) {
				n++
			}
		}
	}
	return n
}

func appendJSONL(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}
```

- [ ] **Step 4: Wire it up**

`server.go` — add to `app` (after `format string`):

```go
	metrics string // JSONL runs file; "" disables
```

In `load`, add a failure recorder and wrap the error returns; time the grouping; append the success record (add `"time"` to imports):

```go
func (a *app) load(ctx context.Context, ref PRRef) error {
	fail := func(err error) error {
		// Failed runs are recorded too — the degraded path must not vanish
		// from the stats (handoff W4).
		if a.metrics != "" {
			_ = appendJSONL(a.metrics, runRecord{
				TS: time.Now().UTC().Format(time.RFC3339), PR: prID(ref), Error: err.Error()})
		}
		return err
	}
	fmt.Fprintf(os.Stderr, "fetching PR #%d from %s/%s…\n", ref.Number, ref.Workspace, ref.Repo)
	pr, err := a.client.FetchPR(ctx, ref)
	if err != nil {
		return fail(err)
	}
	diff, err := a.client.FetchDiff(ctx, ref)
	if err != nil {
		return fail(err)
	}
	segs := SplitUnifiedDiff(diff)
	paths := segmentPaths(segs)
	if len(paths) == 0 {
		return fail(errors.New("PR has no diff to review"))
	}
	fmt.Fprintf(os.Stderr, "diff: %d files, %d KB\n", len(paths), len(diff)/1024)
	fmt.Fprintf(os.Stderr, "grouping into cohorts (model %s)…\n", a.model)
	start := time.Now()
	g := groupCohorts(ctx, a.group, pr, segs, a.budget)
	fmt.Fprintf(os.Stderr, "%d cohorts\n", len(g.Cohorts))
	if a.metrics != "" {
		if err := appendJSONL(a.metrics, computeRecord(ref, a.model, pr, g, segs, time.Since(start).Seconds())); err != nil {
			fmt.Fprintf(os.Stderr, "cohort-review: metrics: %v\n", err)
		}
	}
	page, err := renderPage(pr, g, a.format)
	if err != nil {
		return fail(err)
	}
	a.mu.Lock()
	a.page, a.diff = page, diff
	a.mu.Unlock()
	return nil
}
```

`main.go` — add `"path/filepath"` to imports; add the flag next to the others:

```go
	defMetrics := ""
	if home, err := os.UserHomeDir(); err == nil {
		defMetrics = filepath.Join(home, ".cohort-review", "runs.jsonl")
	}
	metrics := fs.String("metrics", defMetrics, "append one JSONL record per run here (empty disables)")
```

and set `metrics: *metrics` in the `app` literal inside the credential loop.

- [ ] **Step 5: Run the full suite — expect PASS**

```bash
go test ./...
```

- [ ] **Step 6: Commit**

```bash
git add metrics.go metrics_test.go server.go server_test.go main.go
git commit -m "feat(metrics): per-run JSONL records with misc share and anchor stats"
```

---

## Task 8: W2.3 page — type badges, claim line, collapsed mechanical/deletion

Semantic misc already arrives first from the server; the page adds badges, the claim line, default-collapsed folds, and misc styling. (No representative-specific UI — the existing big-file placeholders and collapsed defaults carry the attention cue.)

**Files:**
- Modify: `page.tmpl`
- Test: `server_test.go`

- [ ] **Step 1: Extend the round-trip test**

In `TestRenderPageDataRoundTrips`, replace the grouping construction and add assertions:

```go
	g := Grouping{
		Walkthrough: "Does things.",
		Cohorts: []Cohort{
			{Name: "Core", Summary: "the core", Claim: "does it work?", Type: "claim", Files: []FileRef{{Path: "a.go"}}},
			{Name: "Dels", Summary: "old suite", Claim: "is the removal complete?", Type: "deletion",
				Files: []FileRef{{Path: "old/x.go"}, {Path: "old/y.go"}}},
		},
	}
```

and after the existing mismatch check:

```go
	if got.Cohorts[0].Claim != "does it work?" || got.Cohorts[0].Type != "claim" {
		t.Errorf("claim/type lost: %+v", got.Cohorts[0])
	}
	if got.Cohorts[1].Type != "deletion" {
		t.Errorf("type lost: %+v", got.Cohorts[1])
	}
```

Also update `testGrouping()` (used by other server tests):

```go
func testGrouping() Grouping {
	return Grouping{
		Walkthrough: "Does things.",
		Cohorts:     []Cohort{{Name: "Core", Summary: "the core", Claim: "does it work?", Type: "claim", Files: []FileRef{{Path: "a.go"}}}},
	}
}
```

Run: `go test ./... -run 'TestRenderPage|TestServerEndpoints'` — expect PASS (fields ride on Cohort's json tags; this pins them).

- [ ] **Step 2: Edit `page.tmpl` — CSS**

Add after the `.coh-sum` rule:

```css
  .coh-claim { font-size: 13px; color: var(--ink-2); margin: -6px 0 12px 21px; line-height: 1.5; }
  .coh-claim b { color: var(--accent); font-weight: 650; }
  .kpill { font-family: var(--mono); font-size: 10px; font-weight: 600; border-radius: 20px;
    padding: 2px 8px; letter-spacing: .04em; flex: none; }
  .kpill.mechanical, .kpill.deletion { background: var(--sidebar); color: var(--muted); border: 1px solid var(--border); }
  .kpill.nonfix { background: var(--accent-soft); color: var(--accent); }
  .kpill.misc { background: var(--accent-soft); color: var(--del-ink); border: 1px solid var(--del-ink); }
  .cohort.misc .coh-head h2 { color: var(--del-ink); }
```

- [ ] **Step 3: Edit `page.tmpl` — JS**

Above `build()` add:

```js
  // Type badges; "claim" is the default and gets no badge. Types come from the
  // server-side enum (validType), so they are safe as class names.
  const TYPES = { mechanical: "mechanical", deletion: "deletion", nonfix: "nonfix", misc: "undeclared ⚠" };
```

In `build()`, replace the `sections.insertAdjacentHTML` call with:

```js
      const ctype = TYPES[c.type] ? c.type : "";
      // Handoff W2.3: mechanical and deletion cohorts start collapsed —
      // verifying the claim rarely needs the full diff open.
      const startCollapsed = c.type === "mechanical" || c.type === "deletion";
      sections.insertAdjacentHTML("beforeend",
        `<section class="cohort${ctype ? " " + ctype : ""}${startCollapsed ? " collapsed" : ""}" id="coh-${ci}" data-ci="${ci}">
          <div class="coh-head" onclick="this.parentNode.classList.toggle('collapsed')">
            <span class="swatch" style="background:${c.color}"></span>
            <h2>${esc(c.name)}</h2>
            ${ctype ? `<span class="kpill ${ctype}">${TYPES[ctype]}</span>` : ""}
            <span class="stat"><span style="color:var(--add-ink)">+${cAdd}</span>
              <span style="color:var(--del-ink)">−${cDel}</span>
              <span>${parsed.length} files</span><span class="chev">▾</span></span>
          </div>
          <p class="coh-sum">${esc(c.summary || "")}</p>
          ${c.claim ? `<p class="coh-claim"><b>claim</b> ${esc(c.claim)}</p>` : ""}
          <div class="coh-body"><div class="d2h-wrap" id="wrap-${ci}"><div class="coh-pending">rendering…</div></div></div>
        </section>`);
```

- [ ] **Step 4: Run the full suite + build**

```bash
go test ./... && go build -o cohort-review .
```

- [ ] **Step 5: Commit**

```bash
git add page.tmpl server_test.go
git commit -m "feat(page): type badges, claim line, collapsed mechanical and deletion cohorts"
```

---

## Task 9: Docs

**Files:**
- Modify: `CLAUDE.md`, `README.md`, `docs/TODO.md`

- [ ] **Step 1: `CLAUDE.md`** — in Hard constraints, extend the repair-rules bullet's first sentence with folding semantics (repair itself is unchanged):

> Folding (`fold.go`) compresses the MODEL INPUT only — DELETED/SIMILAR summary lines replace folded hunks, but every file stays in the changed-files list and the model still groups it. Repair rules in `cohorts.go` guarantee every hunk of every changed file lands in exactly one cohort (semantic-misc cohorts float first; the unclassified fallback bucket stays last); keep that invariant — `TestRepairHunkPartition` pins it, and the page's whole-at-first-ref fallback is its JS half.

Add one bullet:

> - **Metrics never block**: `-metrics` (default `~/.cohort-review/runs.jsonl`) appends one record per run, including failed runs; any write failure only logs.

- [ ] **Step 2: `README.md`** — add to the usage/flags section:

> - `-metrics <path>` — append one JSON record per run (misc_lines_pct, folded_lines_pct, anchor stats, cohort types) to `<path>`; defaults to `~/.cohort-review/runs.jsonl`, empty disables.
>
> Cohorts carry a type: **claim** (answer the one-question claim), **mechanical** / **deletion** (folded churn; start collapsed — verify the representative / answer the three deletion questions), **nonfix** (review the decision not to fix), **misc** (undeclared by the description — floats first; ask the author). `make golden` replays the three stored golden PRs for human comparison (see `testdata/golden/`).

- [ ] **Step 3: `docs/TODO.md`** — replace the "Mechanical-cohort flag" section body with:

> Superseded (2026-09-01) by the M1 type taxonomy: cohorts carry `type`, mechanical/deletion cohorts render pre-collapsed, and prompt-side folding compresses their model input. See `docs/superpowers/plans/2026-09-01-m1-review-tour-gaps.md`.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md README.md docs/TODO.md
git commit -m "docs: fold/claim/misc/metrics semantics for M1"
```

---

## Task 10: W6 golden eval harness

Fetch once, replay offline (diff from disk; the grouping call still uses the real `claude` CLI — that's the point of the harness). Judgment is human: print actual vs expected.

**Files:**
- Create: `scripts/fetch-golden.sh`
- Create: `golden_test.go` (build tag `golden`)
- Create: `Makefile`
- Create: `testdata/golden/expected/apigw-18.md`, `testdata/golden/expected/backburner-7.md`, `testdata/golden/expected/shopline-16595.md`

- [ ] **Step 1: The fetch script**

`scripts/fetch-golden.sh` (then `chmod +x`):

```bash
#!/usr/bin/env bash
# Fetch one golden PR's metadata + diff into testdata/golden/<name>/.
# Usage: scripts/fetch-golden.sh <name> <workspace> <repo> <pr-id>
set -euo pipefail
cd "$(dirname "$0")/.."
name=$1 ws=$2 repo=$3 id=$4
if [ -f ./.env ]; then set -a; source ./.env; set +a; fi
: "${BITBUCKET_EMAIL:?set BITBUCKET_EMAIL}" "${BITBUCKET_API_TOKEN:?set BITBUCKET_API_TOKEN}"
dir="testdata/golden/$name"
mkdir -p "$dir"
curl -fsS -u "$BITBUCKET_EMAIL:$BITBUCKET_API_TOKEN" \
  "https://api.bitbucket.org/2.0/repositories/$ws/$repo/pullrequests/$id" -o "$dir/pr.json"
curl -fsSL -u "$BITBUCKET_EMAIL:$BITBUCKET_API_TOKEN" \
  "https://api.bitbucket.org/2.0/repositories/$ws/$repo/pullrequests/$id/diff" -o "$dir/diff.txt"
echo "saved $dir"
```

- [ ] **Step 2: The tag-gated replay test**

`golden_test.go`:

```go
//go:build golden

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestGolden replays the stored golden PRs through the real grouping call
// (claude CLI; diff and metadata from disk, no network) and prints actual vs
// expected for HUMAN judgment (handoff W6: 判分不用全自動). It fails only on
// harness errors, never on grouping content. Run via `make golden`.
func TestGolden(t *testing.T) {
	dirs, _ := filepath.Glob("testdata/golden/*/")
	var fixtures []string
	for _, d := range dirs {
		if filepath.Base(filepath.Clean(d)) != "expected" {
			fixtures = append(fixtures, d)
		}
	}
	if len(fixtures) == 0 {
		t.Fatal("no golden fixtures — run scripts/fetch-golden.sh first")
	}
	for _, dir := range fixtures {
		name := filepath.Base(filepath.Clean(dir))
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, "pr.json"))
			if err != nil {
				t.Fatal(err)
			}
			var pr struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			}
			if err := json.Unmarshal(raw, &pr); err != nil {
				t.Fatal(err)
			}
			diff, err := os.ReadFile(filepath.Join(dir, "diff.txt"))
			if err != nil {
				t.Fatal(err)
			}
			segs := SplitUnifiedDiff(string(diff))
			group := func(ctx context.Context, prompt string) (string, error) {
				fmt.Printf("[%s] prompt: %d KB total\n", name, len(prompt)/1024)
				return runClaude(ctx, "sonnet", prompt)
			}
			g := groupCohorts(context.Background(), group,
				PullRequest{Title: pr.Title, Description: pr.Description}, segs, defaultBudget)
			fmt.Printf("\n===== %s: ACTUAL (%d cohorts, %d lines folded, degraded=%v) =====\n",
				name, len(g.Cohorts), g.FoldedLines, g.Degraded)
			for i, c := range g.Cohorts {
				fmt.Printf("%d. [%s] %s — %s\n   claim: %s\n", i+1, c.Type, c.Name, c.Summary, c.Claim)
				for _, f := range c.Files {
					fmt.Printf("   - %s\n", refString(f))
				}
			}
			if exp, err := os.ReadFile(filepath.Join("testdata/golden/expected", name+".md")); err == nil {
				fmt.Printf("===== %s: EXPECTED =====\n%s\n", name, exp)
			}
		})
	}
}
```

- [ ] **Step 3: Makefile**

```make
golden:
	go test -tags golden -run TestGolden -v -timeout 30m .

.PHONY: golden
```

- [ ] **Step 4: Expected files** (from the handoff appendix — these ARE the ground truth)

`testdata/golden/expected/apigw-18.md`:

```markdown
# callback-service-api-gateway#18 — 平行意圖型 (609 行 · 9 檔 · easy mode)
7 組:
1. [claim] 主幹: 非 JSON body → form fallback — flag 兩個 yaml + resolveBodyFormat +
   transformFormPayload + wrapper/isJSONObject + 配套測試 (~250 行)
2. [claim] JSON root 類型 — object/string 過; array/number/bool/null → ErrBypassSender
3. [claim] 空 body → {}
4. [claim] Family Mart 錯誤 → 200 fallback
5. [claim] content-type 統一 application/json
6. [mechanical] NIT: pulsar log 刪 1 行 + DisableDefaultContentType 機械加進 ~10 個測試 context
7. [misc] payload string→[]byte 型別重構 — description 沒宣告 (應排最前)

關鍵: order_payment_with_custom_response.yaml 一檔跨 ①⑤ 兩組 (hunk-level);
fallbackResponse() 同時服務 ④⑤, 雙屬可接受 (v1 first-claim-wins, 可容忍).
```

`testdata/golden/expected/backburner-7.md`:

```markdown
# backburner#7 — 主從結構型 (918 行 · 16 檔 · description 空白, hard mode)
5 組 + misc, 讀序 = 機制 → 接線 → 善後:
1. [claim] 核心模組 OtelJobTracing + 專屬測試 + CI otel-test step (~59%)
2. [claim] producer 接線 — worker.rb enqueue + worker_test
3. [claim] consumer 接線 — job.rb process + job_test
4. [claim] 子行程退出前 flush — configuration + flush_open_telemetry +
   forking/threads_on_fork + 兩個 worker test
5. [nonfix] facade 契約 — backburner.rb 文件 + README facade 段 + back_burner_test 釘現狀
   (pre-existing 問題刻意不修, README + 測試當緩解)
misc: string→[]byte 型別重構 (transformer.go + basic_transformer.go) — 排最前

關鍵: description 0 字, CHANGELOG 12 行是作者的意圖摘要 (W5 in-diff 錨點實例).
```

`testdata/golden/expected/shopline-16595.md`:

```markdown
# api.shoplineapp.com#16595 — 刪除型 (15,393 行 · 219 檔 · 96.6% 刪除)
3 組:
1. [claim] trigger_qa_api_test.sh 新腳本 (403 行, 唯一逐行 review 對象)
2. [claim] pipeline 換心臟 (113 行 yml) — 藏最重要決策 on-fail: strategy: ignore 軟上線
3. [deletion] 217 檔 vendored Karate suite (14,877 行) — claim 三問

驗收重點: 摺疊生效 — 送模型的 hunk 內容 < 30KB (prompt 大小印在 harness 輸出);
217 刪除檔歸一個 deletion cohort.
```

- [ ] **Step 5: Fetch the fixtures** (needs `./.env` creds and the workspace names — ask the author):

```bash
scripts/fetch-golden.sh apigw-18 <workspace> callback-service-api-gateway 18
scripts/fetch-golden.sh backburner-7 <workspace> backburner 7
scripts/fetch-golden.sh shopline-16595 <workspace> api.shoplineapp.com 16595
```

Add `testdata/golden/*/diff.txt` and `pr.json` to git only if the author approves (proprietary diffs; default: add `testdata/golden/*/` to `.gitignore`, keep `expected/` tracked):

```bash
printf 'testdata/golden/*/\n!testdata/golden/expected/\n' >> .gitignore
```

- [ ] **Step 6: Verify tags don't leak into normal builds, then commit**

```bash
go vet ./... && go test ./...    # golden_test excluded without the tag — expect PASS
git add scripts/fetch-golden.sh golden_test.go Makefile testdata/golden/expected .gitignore
git commit -m "feat(golden): offline golden replay harness with human-judged output"
```

---

## Task 11: M1 acceptance checklist (manual, needs the author)

The handoff's checklist verbatim. Needs `./.env` creds + logged-in `claude` CLI + the author's judgment — **notify yui and hand over**.

- [ ] `make golden` runs all three; outputs compared against `testdata/golden/expected/*.md`
- [ ] Each golden also runs live: `./cohort-review <url>` for all three PRs
- [ ] #16595: folding works — prompt hunk content **< 30KB** (harness prints it; was ~200KB), 217 deleted files land in ONE `deletion` cohort
- [ ] backburner#7: facade group typed `nonfix`; string→[]byte refactor lands in semantic misc and sits **first**
- [ ] #18: NIT group typed `mechanical` and starts collapsed; misc ≈ the ~8-line type refactor
- [ ] `~/.cohort-review/runs.jsonl` gains three records with `misc_lines_pct` and `anchor` fields (`tail -3 ~/.cohort-review/runs.jsonl`)
- [ ] Optional: re-capture `docs/screenshot.png` during a golden run — the current image predates badges/claims/collapsed cohorts
- [ ] Similar-fold observability: run once against a 100+-file same-shape PR (e.g. operational-bill-service#1328) and record group count, member counts, and the A/M status mix of members — this settles whether status-A (new) files should be excluded from similar folds and whether exact-dir keying captures the real clusters
- [ ] Degradation tests still green (LLM failure → "All changes"); each new behavior has a test
- [ ] `go vet ./...` and `go test ./...` fully green
- [ ] Author rating recorded per PR (edit this section): pass at ≥「跟我想的類似」on all three. A miss = tune prompt rules / `minFoldSize`, rerun that golden only — not a redesign.
- [ ] Wrap up via superpowers:finishing-a-development-branch (merge/PR decision belongs to the author)

---

## Task 12 (OPTIONAL — handoff W5, "time permitting"): anchors

Skip unless the author asks for it after Task 11; it is independent of everything above.

- [ ] **Step 1: In-diff anchor prompt rule** — in `buildCohortPrompt`, after the description-as-context rule:

```go
	b.WriteString("- Treat in-diff intent summaries — CHANGELOG entries, new doc comments, config-file comments — as author-written anchors for naming cohorts and claims, but verify them against the code: they rank below the PR description in trustworthiness.\n")
```

Test (append to `TestClaimTypeRulesInPrompt` fragments): `"CHANGELOG"`.

- [ ] **Step 2: Jira best-effort** — new file `jira.go`:

```go
// Best-effort Jira anchor (handoff W5): if the PR title carries a Jira key and
// JIRA_BASE_URL + JIRA_EMAIL + JIRA_API_TOKEN are set, fetch the issue's
// summary and description as an extra anchor. Any failure is silent — anchors
// are optional, the page never waits on Jira correctness.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func fetchJiraAnchor(ctx context.Context, title string) string {
	key := jiraKeyRe.FindString(title)
	base, email, token := os.Getenv("JIRA_BASE_URL"), os.Getenv("JIRA_EMAIL"), os.Getenv("JIRA_API_TOKEN")
	if key == "" || base == "" || email == "" || token == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET",
		strings.TrimSuffix(base, "/")+"/rest/api/2/issue/"+key+"?fields=summary,description", nil)
	if err != nil {
		return ""
	}
	req.SetBasicAuth(email, token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var out struct {
		Fields struct {
			Summary     string `json:"summary"`
			Description string `json:"description"`
		} `json:"fields"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%s: %s\n\n%s", key, out.Fields.Summary, out.Fields.Description))
}
```

Wire: `PullRequest` gains `Jira string \`json:"-"\``; in `load` after `FetchPR`: `pr.Jira = fetchJiraAnchor(ctx, pr.Title)`; `promptInput` gains `Jira string`; `groupCohorts` sets `in.Jira = pr.Jira`; `buildCohortPrompt` fences it when non-empty (after the PR title/description section):

```go
	if strings.TrimSpace(in.Jira) != "" {
		b.WriteString("## Jira issue (anchor — verify against the diff)\n")
		b.WriteString(Fence(nonce, "jira issue", in.Jira))
		b.WriteString("\n\n")
	}
```

Test with `httptest` fake (set the three env vars via `t.Setenv` pointing at the fake; assert anchor content reaches the prompt through a canned `groupFunc`, mirroring `TestLoadThreadsPRMetadataIntoPrompt`), plus a `TestJiraAnchorSilentWithoutCreds` asserting `""` with unset env.

- [ ] **Step 3: Commit**

```bash
git add jira.go jira_test.go cohorts.go cohorts_test.go bitbucket.go server.go
git commit -m "feat(anchors): in-diff anchor rule and best-effort jira issue anchor"
```

---

## Review-driven amendments (normative — supersede the embedded code blocks above)

- **Task 1** (applied in its commit): `diffsplit.go` parity header enumerates BOTH new-here functions ("deriveNameStatus and segmentStat are new here"); `TestSegmentStat` carries two extra mutation-killing cases (diff-of-a-diff `+++/---` guard; pre-hunk noise pinning the `inHunk` gate).
- **Task 2** (applied in its commit): the misc prompt rule ends with an empty-anchor guard — "When the title and description declare nothing, infer the PR's main intents from the diff itself and reserve \"misc\" for changes serving none of them — a missing description never turns the whole PR into misc." (protects the empty-description golden backburner#7 and the `misc_lines_pct` metric); the claim rule reads "the question … (normally ONE; \"deletion\" below is the exception)"; the JSON shape line shows the full enum `"type": "claim|mechanical|deletion|nonfix|misc"`; `Cohort.Claim`'s doc comment cites "handoff W2" instead of the untraceable 三次盲測 phrase; `TestTypeAndClaimParsedValidated` has a third cohort (no `type` key → "claim").
- **Task 3** (applied in its commit): `fold_test.go` gains `TestFoldOnlyDeletions`, pinning the status-D filter via the deletion fold's membership (deliberately robust to Task 4 turning the co-located modified files into a "similar" fold).
- **Task 4** (applied in its commit): `TestFoldSimilarKeepsRepresentative` gains two non-member fixtures pinning the Dir and Ext key components; new `TestFoldSimilarRepTieFirstWins` (strict `>` tie-break); zero-churn similar groups (pure renames) are skipped with `TestFoldSimilarSkipsZeroChurn` + `renSeg` helper; a why-comment records the topDir-vs-path.Dir asymmetry and the inFold ordering assumption. OPEN QUESTION (do not resolve without data): should status-A files be excluded from similar folds? Settled by the Task 11 observability run against a #1328-shaped PR. Task 5's SIMILAR prompt rule was softened to claim only path/ext/size similarity, not content equivalence.
- **Task 5 watch-notes** (from Task 3's review, for Task 5's reviewer): (a) a real #16595 DELETED line is ~7-8KB on one line — confirm the all-or-nothing budget check can't plausibly dump 217 members into Omitted under the default 200KB budget, and that the Omitted fallback stays honest when it does fire; (b) `Fold.Lines` is uniformly member churn (deletion members have added=0 by construction), so `folded_lines_pct` mixes nothing — no change needed, just don't "fix" it.
- **Task 5** (applied in its commit): new tests `TestFoldedPromptOverBudgetDegradesToOmitted` (pins the budget guard + honest Omitted fallback), `TestFoldLineAtFirstMemberPosition` (pins first-member emission), `Degraded` asserted in both groupCohorts happy/fallback tests; the `delete(foldAt, …)` defensive comment restored. Verified in review: simulated #16595 drops model hunk content 199.5KB → 15.3KB with 0 omitted files (vs 157 unfolded) — W1's <30KB gate holds with margin. Accepted observations, no change without data: the over-budget Omitted fallback trades structure for near-zero byte savings but preserves the Hunks≤budget contract; a SIMILAR rep can itself be omitted at extreme budgets (rules stay individually true); comma-containing hostile filenames can pad fold-line member lists (bounded — resolveRef drops fakes, partition holds; same class as the pre-existing Omitted/Truncated NOTEs).
- **Task 11 addendum**: alongside the <30KB check, record the omitted-file count on #16595 (expect 0 folded vs ~157 unfolded — the more legible signal).
- **Task 6** (applied in its commit): new `TestSemanticMiscOrderStableAndExclusive` (multi-misc relative order + nonfix never floats) with `cohortNames` helper; W3 comment wording corrected ("fallback bucket appended below"); **behavior change** — `groupCohorts`' success gate now requires at least one typed (model-derived) cohort, so a model answering `{"cohorts": []}` retries once and then degrades (`Degraded=true`) instead of being recorded as a successful grouping; pinned by `TestGroupCohortsRetriesWhenModelGroupsNothing` / `TestGroupCohortsEmptyGroupingsDegrade`. This resolves the Task 7 question about empty-model-output runs looking successful in runs.jsonl — they now carry the degraded error.
- **Task 7** (applied in its commit): `TestComputeRecordChurnEdges` (non-uniform hunks + diff-of-a-diff guard + NaN/zero-churn marshal pin — the plan's own uniform 2-line fixture had let three mutants live); metrics.go header documents the multi-line-per-run property and the analysis tell (full records carry `cohorts`/`anchor`; filter on `has("cohorts")` before averaging `misc_lines_pct`); `hunkChurn` reuses `segmentStat` per hunk instead of duplicating the churn rules; `desc_chars` counts runes (CJK); jira-regex FP class documented (inflates anchor presence); load/flag doc strings say "per load attempt". Accepted without change: float precision noise, silent disable on UserHomeDir failure, credential-retry lines.
- **Task 8 watch-note**: with misc floating first, an unsplittable multi-ref path renders its whole block at its FIRST ref, which may now be the misc cohort (exactly-once invariant unaffected; stats/colors shift). Misc styling may therefore wrap a large whole-file block — acceptable, just don't "fix" the first-ref rule.
- **Task 8** (applied in its commit): collapsed cohorts defer their diff draw to first expand (`toggleCohort` + renderCohort collapsed-guard + jump/jumpFile reorder — all four edits are load-bearing together); light-mode mechanical/deletion pill uses `--ink-2` (AA); `Object.hasOwn` type lookup; `TYPE_HELP` badge tooltips; `page_script_test.go` shells `node --check` over the inline scripts (skips without node) — note the filename: `page_js_test.go` is a GOOS=js build constraint and gets silently excluded. Accepted: empty-summary margin rhythm, keyboard a11y (localhost tool), setFmt re-collapse, nav ⚠ deferred to the golden run.
- **Task 9** (applied in its commit): README/CLAUDE.md use `--metrics` style; "per load attempt" wording (a run can write two lines); CLAUDE.md documents the silent-drop error-line write and the node smoke test; README gains a folding Notes bullet and a widened mechanical description; the plan's `make golden` README sentence deliberately omitted until Task 10 lands. Known-stale: `docs/screenshot.png` predates the M1 UI (refresh optional, Task 11).
- **Task 10** (applied in its commit): the plan's `filepath.Glob("testdata/golden/*/")` NEVER matches (Go's Glob rejects a trailing separator) — fixture discovery uses `os.ReadDir` with an `IsDir()` + `!= "expected"` filter instead; `make golden` carries `-count=1` (go test caching would otherwise replay stale model output); fetch script gained usage-on-missing-args, `--remove-on-error`, a file/line-count echo comparable to the expected headers, and an `.env` format note; missing expected files print loudly; README documents `make golden`. The test-file naming trap from Task 8 (`_js`/`_wasm` etc. suffixes are GOOS/GOARCH build constraints) generalizes: never end a Go filename in `_<goos>` or `_<goarch>`.
- General: when a task adds a new-here function to a copied-parity file, its header enumeration must be updated in the same commit.

## Self-review notes

- **Handoff coverage**: W1 → Tasks 3/4/5 (prompt-side folding, all paths listed, greedy budget kept as backstop, <30KB acceptance observable via the stderr/harness size print); W2 → Tasks 2/8 (Claim question-form + Type, deletion three questions, nonfix rule, badges, collapsed defaults); W3 → Task 6 (semantic misc first, fallback bucket renamed and kept last — two buckets, not merged); W4 → Task 7 (exact schema incl. error lines, `runs.jsonl`); W6 → Task 10; W5 → Task 12 (optional, as the handoff marks it); acceptance checklist → Task 11 (incl. `go vet`).
- **Deliberate deviations from the handoff, stated**: (a) W1 says "1–2 representative files" — this plan sends exactly 1 (the largest-churn member); bump to 2 only if a golden shows the model needs it. (b) Deletion folding gets the same `minFoldSize = 5` threshold as similar folds — the handoff names none; folding 1–4 deleted files would hide informative hunks for no budget win. (c) The handoff's example JSONL shows `"jira_key":"PLAT-2328"` under anchor — recorded from the title regex in Task 7 even though Jira *fetching* is optional Task 12.
- **Type consistency**: `Cohort{Name, Summary, Claim, Type, Files}` / `Fold{Kind, Members, Rep, Dir, Lines}` / `Grouping{Walkthrough, Cohorts, Degraded, FoldedLines}` / `runRecord`/`anchorStats`; functions `segmentStat`, `topDir`, `magnitude`, `foldSegments`, `foldLine`, `validType`, `buildPromptInput(segs, folds, budget)`, `computeRecord`, `hunkChurn`, `appendJSONL`, `prID`, `fetchJiraAnchor` — later tasks use exactly these names.
- **Invariant check**: repair is untouched end-to-end; `TestRepairHunkPartition` is never edited; `TestGroupCohortsFoldedLines` pins that folded-but-unclaimed files still get swept, so the partition holds with folding active.
