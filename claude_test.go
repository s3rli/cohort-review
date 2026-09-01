package main

import (
	"errors"
	"strings"
	"testing"
)

func TestClaudeFailure(t *testing.T) {
	base := errors.New("exit status 1")
	// stderr wins when present.
	err := claudeFailure(base, []byte(`{"result":"ignored"}`), []byte("boom"))
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("stderr not surfaced: %v", err)
	}
	// Empty stderr: the JSON error envelope on stdout is the real message.
	err = claudeFailure(base, []byte(`{"result":"Prompt is too long","is_error":true}`), nil)
	if !strings.Contains(err.Error(), "Prompt is too long") {
		t.Errorf("stdout envelope not surfaced: %v", err)
	}
	// Non-envelope stdout still shown raw.
	err = claudeFailure(base, []byte("raw failure text"), nil)
	if !strings.Contains(err.Error(), "raw failure text") {
		t.Errorf("raw stdout not surfaced: %v", err)
	}
}

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

func TestExtractClaudeResultArrayEnvelope(t *testing.T) {
	// claude CLI 2.x emits an array of message objects; the answer lives in
	// the "type":"result" element (observed on 2.1.252).
	out := `[{"type":"system","subtype":"init"},{"type":"assistant"},{"is_error":false,"subtype":"success","result":"{\"walkthrough\": \"w\", \"cohorts\": []}","type":"result"}]`
	if got := extractClaudeResult([]byte(out)); got != `{"walkthrough": "w", "cohorts": []}` {
		t.Errorf("array envelope not unwrapped: %q", got)
	}
	// An array with no result element stays raw (idempotence contract).
	noResult := `[{"type":"system"}]`
	if got := extractClaudeResult([]byte(noResult)); got != noResult {
		t.Errorf("no-result array mangled: %q", got)
	}
}
