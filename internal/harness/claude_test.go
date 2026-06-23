// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"strings"
	"testing"
)

// Claude Code emits one JSON event per line under --output-format stream-json
// --verbose: assistant events carry tool_use content blocks, and a terminal
// type:"result" event carries the final answer, usage, cost, and error
// envelope. These fixtures mirror that shape (the same one ScanLine parses).
const (
	claudeStreamSuccess = `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"Creating the file."}]}}
{"type":"assistant","message":{"id":"m2","content":[{"type":"tool_use","id":"t1","name":"Write","input":{"file_path":"foo.txt","content":"hello"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
{"type":"assistant","message":{"id":"m3","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"terraform plan"}}]}}
{"type":"result","subtype":"success","is_error":false,"result":"Done.","total_cost_usd":0.0123,"usage":{"input_tokens":100,"cache_creation_input_tokens":20,"cache_read_input_tokens":50,"output_tokens":30}}`

	claudeStreamNoTools = `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}
{"type":"result","subtype":"success","is_error":false,"result":"hi","usage":{"input_tokens":5,"output_tokens":2}}`

	claudeStreamMaxTurns = `{"type":"system","subtype":"init"}
{"type":"result","subtype":"error_max_turns","is_error":true,"result":"","errors":["hit max turns"]}`
)

func TestClaudeParseToolCalls(t *testing.T) {
	c := NewClaude()
	calls := c.ParseToolCalls([]byte(claudeStreamSuccess))
	if len(calls) != 2 {
		t.Fatalf("ParseToolCalls = %d calls, want 2: %+v", len(calls), calls)
	}
	if calls[0].Name != "Write" || !strings.Contains(string(calls[0].Input), `"foo.txt"`) {
		t.Errorf("call[0] = %+v, want Write with foo.txt", calls[0])
	}
	if calls[1].Name != "Bash" || !strings.Contains(string(calls[1].Input), "terraform plan") {
		t.Errorf("call[1] = %+v, want Bash with terraform plan", calls[1])
	}

	// A run with no tool_use blocks reports nil; the engine normalizes that to a
	// non-nil empty slice so a tool_call assertion fails rather than skips.
	if got := c.ParseToolCalls([]byte(claudeStreamNoTools)); got != nil {
		t.Errorf("ParseToolCalls(no tools) = %+v, want nil", got)
	}
	if got := c.ParseToolCalls([]byte("not json\n")); got != nil {
		t.Errorf("ParseToolCalls(garbage) = %+v, want nil", got)
	}
}

func TestClaudeParseEvalOutput(t *testing.T) {
	c := NewClaude()
	text, usage := c.ParseEvalOutput([]byte(claudeStreamSuccess))
	if text != "Done." {
		t.Errorf("text = %q, want %q", text, "Done.")
	}
	if usage == nil {
		t.Fatal("usage = nil, want populated")
	}
	// Fresh input, cache read, and cache write stay on their own fields.
	if got := derefInt(usage.InputTokens); got != 100 {
		t.Errorf("InputTokens = %d, want 100", got)
	}
	if got := derefInt(usage.CacheReadTokens); got != 50 {
		t.Errorf("CacheReadTokens = %d, want 50", got)
	}
	if got := derefInt(usage.CacheCreationTokens); got != 20 {
		t.Errorf("CacheCreationTokens = %d, want 20", got)
	}
	if got := derefInt(usage.OutputTokens); got != 30 {
		t.Errorf("OutputTokens = %d, want 30", got)
	}
	if usage.CostUSD == nil || *usage.CostUSD != 0.0123 {
		t.Errorf("CostUSD = %v, want 0.0123", usage.CostUSD)
	}

	// No result event: fall back to raw stdout with nil usage.
	raw := "plain text answer\n"
	if text, usage := c.ParseEvalOutput([]byte(raw)); text != raw || usage != nil {
		t.Errorf("ParseEvalOutput(plain) = (%q, %v), want (%q, nil)", text, usage, raw)
	}
}

func TestClaudeRuntimeError(t *testing.T) {
	c := NewClaude()
	tests := []struct {
		name     string
		stdout   string
		exitCode int
		want     string
	}{
		{"gradable result", claudeStreamSuccess, 0, ""},
		{"empty", "", 1, "empty CLI output"},
		{"plain text clean exit", "hello\n", 0, ""},
		{"plain text crash", "boom\n", 1, "unparseable CLI output"},
		{"max turns empty result", claudeStreamMaxTurns, 1, "claude run error (error_max_turns): hit max turns"},
	}
	for _, tt := range tests {
		if got := c.RuntimeError([]byte(tt.stdout), tt.exitCode, false); got != tt.want {
			t.Errorf("%s: RuntimeError = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestClaudeScanLine(t *testing.T) {
	c := NewClaude()
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"skill tool_use", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":{"command":"my-skill"}}]}}`, true},
		{"read skill.md", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"skills/my-skill/SKILL.md"}}]}}`, true},
		{"unrelated tool", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"x"}}]}}`, false},
		{"garbage", "not json", false},
	}
	for _, tt := range tests {
		if hit, _ := c.ScanLine([]byte(tt.line), "my-skill"); hit != tt.want {
			t.Errorf("%s: ScanLine = %v, want %v", tt.name, hit, tt.want)
		}
	}
}

func derefInt(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}
