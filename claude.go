// Adapted from code-review-agent internal/agent/claude.go — keep parity for
// same-source convergence. Collapsed to a single grouping call: hardcoded flag
// set, no telemetry, no tool scopes.
//
// DELIBERATE DIVERGENCE: extractClaudeResult also accepts the CLI's stream-event
// array output. Upstream still assumes the single {"result": …} envelope and is
// broken against current CLI versions the same way this was — carry this fix
// across at convergence rather than reverting it.
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
	// --effort medium curbs the model's thinking: at the CLI's default effort,
	// large prompts can think so long that the server pauses the turn
	// repeatedly and the run dies with error_max_turns (observed on a 214KB
	// prompt: two ~100s failed runs at default effort vs one 21s single-turn
	// success at medium). --max-turns 8 leaves headroom for an occasional
	// pause anyway. --tools "" removes every tool from the request, so a turn
	// can never be burned on a tool call and, with nothing to prompt for,
	// --dangerously-skip-permissions is unnecessary.
	cmd := exec.CommandContext(ctx, "claude",
		"-p",
		"--model", model,
		"--effort", "medium",
		"--max-turns", "8",
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
// --output-format json output. The CLI emits either a single envelope object
// ({"result": "...", ...}) or an array of stream events ending in a "result"
// event carrying the same field — current versions do the latter, and without
// this the whole array reached the JSON parser as if it were the answer.
// Idempotent on non-envelope output.
func extractClaudeResult(output []byte) string {
	var obj struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(output, &obj); err == nil && obj.Result != "" {
		return obj.Result
	}
	var events []struct {
		Type   string `json:"type"`
		Result string `json:"result"`
	}
	if err := json.Unmarshal(output, &events); err == nil {
		// Last one wins: the result event is emitted at the end of the turn.
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].Type == "result" && events[i].Result != "" {
				return events[i].Result
			}
		}
	}
	return string(output)
}
