package main

import "testing"

func TestExtractClaudeResult(t *testing.T) {
	if got := extractClaudeResult([]byte(`{"result": "hello", "usage": {}}`)); got != "hello" {
		t.Errorf("envelope: got %q", got)
	}
	// Idempotent on non-envelope output.
	if got := extractClaudeResult([]byte("plain text")); got != "plain text" {
		t.Errorf("plain: got %q", got)
	}
	if got := extractClaudeResult([]byte(`{"other": 1}`)); got != `{"other": 1}` {
		t.Errorf("no result field: got %q", got)
	}
}
