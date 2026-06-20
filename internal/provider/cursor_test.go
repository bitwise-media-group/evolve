// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import "testing"

func TestRuntimeErrorCursor(t *testing.T) {
	c := NewCursor()
	if got := c.RuntimeError([]byte(`{"result":"all done"}`), 0, false); got != "" {
		t.Errorf("valid result = %q, want gradable", got)
	}
	if got := c.RuntimeError([]byte(""), 1, false); got == "" {
		t.Error("empty stdout must be a runtime error")
	}
	if got := c.RuntimeError([]byte(`{"result":""}`), 1, false); got == "" {
		t.Error("empty result + non-zero exit must be a runtime error")
	}
}

func TestScanLineCursor(t *testing.T) {
	c := NewCursor()
	tests := []struct {
		name     string
		line     string
		skill    string
		wantHit  bool
		wantNote string
	}{
		{
			name:    "readToolCall started",
			line:    `{"type":"tool_call","subtype":"started","call_id":"c1","tool_call":{"readToolCall":{"args":{"path":"/ws/.cursor/skills/go-testing/SKILL.md"}}}}`,
			skill:   "go-testing",
			wantHit: true,
		},
		{
			name:    "assistant prose mentioning path",
			line:    `{"type":"assistant","message":{"content":[{"type":"text","text":"see skills/go-testing/SKILL.md"}]}}`,
			skill:   "go-testing",
			wantHit: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit, note := c.ScanLine([]byte(tt.line), tt.skill)
			if hit != tt.wantHit {
				t.Errorf("hit = %v, want %v", hit, tt.wantHit)
			}
			if note != tt.wantNote {
				t.Errorf("note = %q, want %q", note, tt.wantNote)
			}
		})
	}
}

func TestParseEvalOutputCursor(t *testing.T) {
	text, usage := NewCursor().ParseEvalOutput([]byte(`{"type":"result","subtype":"success","is_error":false,"result":"all done"}`))
	if text != "all done" || usage != nil {
		t.Errorf("got text=%q usage=%v, want %q and nil usage", text, usage, "all done")
	}
}

func TestCursorHasNoCounting(t *testing.T) {
	var p Provider = NewCursor()
	if _, ok := p.(TokenCounter); ok {
		t.Error("cursor must not implement TokenCounter")
	}
	if _, ok := p.(EvalRunner); !ok {
		t.Error("cursor must implement EvalRunner")
	}
}
