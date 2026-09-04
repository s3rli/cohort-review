// Copied from code-review-agent internal/vcs/diffsplit.go, plus diffFileHeader
// from internal/vcs/diffmap.go — keep parity for same-source convergence.
// deriveNameStatus and segmentStat are new here: the CLI has no local
// checkout to run `git diff --name-status` in, so status is derived from
// segment headers.
package main

import (
	"fmt"
	"regexp"
	"strings"
)

// FileSegment is one file's raw slice of a unified diff, headers and hunks
// preserved byte-for-byte. Concatenating all segments' Raw in order reproduces
// the input diff exactly.
type FileSegment struct {
	// Path is the file the segment describes: the new-side path from
	// "+++ b/<path>", or the old-side "--- a/<path>" for deletions
	// ("+++ /dev/null"). Empty for unparseable preamble content.
	Path string
	Raw  string
}

var (
	diffFileHeader    = regexp.MustCompile(`^\+\+\+ b/(.+)$`)
	diffOldFileHeader = regexp.MustCompile(`^--- a/(.+)$`)
)

// SplitUnifiedDiff splits a unified diff into per-file raw segments in original
// order. Segments start at "diff --git" lines; for diffs without them (some
// providers and hand-built fixtures), each "--- " old-file header starts a
// segment instead. Content before the first header is kept as a leading
// segment with an empty Path so the split is always lossless.
func SplitUnifiedDiff(diff string) []FileSegment {
	if diff == "" {
		return nil
	}
	lines := strings.SplitAfter(diff, "\n")
	useGitHeaders := strings.HasPrefix(diff, "diff --git ") || strings.Contains(diff, "\ndiff --git ")

	var segs []FileSegment
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		raw := cur.String()
		segs = append(segs, FileSegment{Path: segmentPath(raw), Raw: raw})
		cur.Reset()
	}
	for _, line := range lines {
		if isSegmentStart(line, useGitHeaders) {
			flush()
		}
		cur.WriteString(line)
	}
	flush()
	return segs
}

// isSegmentStart reports whether line begins a new per-file segment under the
// detected header style.
func isSegmentStart(line string, useGitHeaders bool) bool {
	if useGitHeaders {
		return strings.HasPrefix(line, "diff --git ")
	}
	return strings.HasPrefix(line, "--- ")
}

// segmentPath extracts the file path a raw segment describes; see
// FileSegment.Path for the resolution order.
func segmentPath(raw string) string {
	var oldPath string
	for _, line := range strings.Split(raw, "\n") {
		if m := diffFileHeader.FindStringSubmatch(line); m != nil {
			return m[1]
		}
		if oldPath == "" {
			if m := diffOldFileHeader.FindStringSubmatch(line); m != nil {
				oldPath = m[1]
			}
		}
	}
	return oldPath
}

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
		// Inside a hunk every line carries exactly a one-character prefix, so
		// a "+++"/"---" line here is an ordinary change whose CONTENT starts
		// with "++"/"--" (`++counter;`, a removed YAML `---` separator). File
		// headers cannot appear here: they precede the first "@@ ", and in a
		// diff-of-a-diff an added header reads "++++ b/x". Skipping them
		// undercounted churn, which the fold share guard and metrics rely on.
		switch {
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			deleted++
		}
	}
	return status, added, deleted
}

// deriveNameStatus builds a "status<TAB>path<TAB>+added/-deleted" list from
// segment headers and hunks. The path column is exactly the segment Path (the
// groupable ground truth) — a rename shows only its new-side path so the model
// can copy paths verbatim without ambiguity; the rename itself is visible in
// the hunks. The line counts give the model a size signal for files whose
// hunks are omitted or truncated from the prompt for budget reasons.
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
