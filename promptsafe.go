// Copied from code-review-agent internal/promptsafe/promptsafe.go — keep
// parity for same-source convergence.
//
// Data-fence helpers that wrap developer-controlled text as UNTRUSTED DATA
// inside prompts. Two defences work together: the real fence label carries a
// per-call random nonce (NewNonce) an attacker can't know at authoring time,
// and any literal occurrence of the fixed Marker inside untrusted text is
// neutralized before fencing (Scrub) so the wrapped text cannot contain a
// well-formed BEGIN/END sentinel that closes the data block early.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// Marker is the FIXED, source-visible prefix of every data-fence sentinel. On
// its own it guarantees nothing — it is never used as the actual fence without
// a per-call nonce and without first scrubbing the untrusted text.
const Marker = "UNTRUSTED-INPUT"

// scrubbed is the neutralized form Marker is rewritten to inside untrusted
// text: it still displays harmlessly but no longer matches the fence.
const scrubbed = "UNTRUSTED_INPUT"

// NewNonce returns a fresh hex nonce (64 bits) used to randomize the
// data-fence label for a single prompt build. A read failure is non-fatal: an
// empty nonce still leaves the marker-scrubbing defence intact.
func NewNonce() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Scrub neutralizes any literal occurrence of the fixed Marker inside
// untrusted text.
func Scrub(s string) string {
	return strings.ReplaceAll(s, Marker, scrubbed)
}

// Fence wraps s in a per-call nonce'd sentinel fence so the model can tell
// data from instructions.
func Fence(nonce, label, s string) string {
	return fmt.Sprintf("<<<%s-%s BEGIN %s>>>\n%s\n<<<%s-%s END %s>>>",
		Marker, nonce, label, Scrub(s), Marker, nonce, label)
}
