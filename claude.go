// Adapted from code-review-agent internal/agent/claude.go — keep parity for
// same-source convergence. Collapsed to a single grouping call: hardcoded flag
// set, no telemetry, no tool scopes.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// claudeTimeout mirrors code-review-agent's walkthroughTimeout.
const claudeTimeout = 3 * time.Minute

func runClaude(ctx context.Context, model, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, claudeTimeout)
	defer cancel()
	// --tools "" removes every tool from the request: the grouping call is a
	// single text-only turn, the model cannot burn its turn on a tool call
	// (newer models did exactly that against the old --disallowedTools
	// denylist, dying with error_max_turns), and with no tools there is
	// nothing to prompt for so --dangerously-skip-permissions is unnecessary.
	// --max-turns 2 is a backstop in case the CLI ever counts an internal
	// step as a turn; with no tools a second turn is normally unreachable.
	cmd := exec.CommandContext(ctx, "claude",
		"-p",
		"--model", model,
		"--max-turns", "2",
		"--tools", "",
		"--output-format", "json",
	)
	// Prompt on stdin, never argv: a large diff would exceed the OS
	// single-argument limit and fail the exec.
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", claudeFailure(err, stdout.Bytes(), stderr.Bytes())
	}
	return extractClaudeResult(stdout.Bytes()), nil
}

// claudeFailure formats a CLI failure. With --output-format json the CLI
// reports most errors as a JSON envelope on STDOUT with an empty stderr, so
// stdout is the fallback detail source — without it failures are blank.
func claudeFailure(err error, stdout, stderr []byte) error {
	detail := string(stderr)
	if strings.TrimSpace(detail) == "" {
		detail = extractClaudeResult(stdout)
	}
	if len(detail) > 500 {
		detail = "…" + detail[len(detail)-500:]
	}
	return fmt.Errorf("claude: %w: %s", err, detail)
}

// extractClaudeResult unwraps the assistant text from the Claude CLI's
// --output-format json envelope ({"result": "...", ...}). Idempotent on
// non-envelope output.
func extractClaudeResult(output []byte) string {
	var obj struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(output, &obj); err == nil && obj.Result != "" {
		return obj.Result
	}
	return string(output)
}
