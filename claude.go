// Adapted from code-review-agent internal/agent/claude.go — keep parity for
// same-source convergence. Collapsed to a single grouping call: hardcoded flag
// set, no telemetry, no tool scopes, array-envelope unwrap for CLI 2.x.
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
// --output-format json envelope. Older CLIs emit a single {"result": "...", ...}
// object; 2.x emits an ARRAY of message objects whose "type":"result" element
// carries the same field (observed on 2.1.252) — a deliberate divergence from
// code-review-agent, which predates the array envelope. Idempotent on
// non-envelope output.
func extractClaudeResult(output []byte) string {
	var obj struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(output, &obj); err == nil && obj.Result != "" {
		return obj.Result
	}
	var arr []struct {
		Type   string `json:"type"`
		Result string `json:"result"`
	}
	if err := json.Unmarshal(output, &arr); err == nil {
		// The result element is last in practice; scan backwards to be safe.
		for i := len(arr) - 1; i >= 0; i-- {
			if arr[i].Type == "result" && arr[i].Result != "" {
				return arr[i].Result
			}
		}
	}
	return string(output)
}
