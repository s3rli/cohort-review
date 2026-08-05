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

// All tools are disallowed: the grouping call is a single text-only turn, which
// is also why --dangerously-skip-permissions is unnecessary — with no tools
// available there is nothing to prompt for.
const claudeDisallowedTools = "Bash,Read,Write,Edit,WebFetch,WebSearch,Grep,Glob,Task,NotebookEdit,TodoWrite"

func runClaude(ctx context.Context, model, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, claudeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude",
		"-p",
		"--model", model,
		"--max-turns", "1",
		"--disallowedTools", claudeDisallowedTools,
		"--output-format", "json",
	)
	// Prompt on stdin, never argv: a large diff would exceed the OS
	// single-argument limit and fail the exec.
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		tail := stderr.String()
		if len(tail) > 500 {
			tail = "…" + tail[len(tail)-500:]
		}
		return "", fmt.Errorf("claude: %w: %s", err, tail)
	}
	return extractClaudeResult(stdout.Bytes()), nil
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
