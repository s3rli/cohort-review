// Copied from code-review-agent internal/vcs/diffsplit.go, plus diffFileHeader
// from internal/vcs/diffmap.go — keep parity for same-source convergence.
// deriveNameStatus is new here: the CLI has no local checkout to run
// `git diff --name-status` in, so status is derived from segment headers.
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

// deriveNameStatus builds a git-style "status<TAB>path" list from segment
// headers. The path column is exactly the segment Path (the groupable ground
// truth) — a rename shows only its new-side path so the model can copy paths
// verbatim without ambiguity; the rename itself is visible in the hunks.
func deriveNameStatus(segs []FileSegment) string {
	var b strings.Builder
	for _, s := range segs {
		if s.Path == "" {
			continue
		}
		status := "M"
		for _, line := range strings.Split(s.Raw, "\n") {
			if strings.HasPrefix(line, "@@ ") {
				break
			}
			switch {
			case strings.HasPrefix(line, "new file mode"):
				status = "A"
			case strings.HasPrefix(line, "deleted file mode"):
				status = "D"
			case strings.HasPrefix(line, "rename from "):
				status = "R"
			}
		}
		fmt.Fprintf(&b, "%s\t%s\n", status, s.Path)
	}
	return b.String()
}
