// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import "testing"

func TestRuntimeErrorOpenAI(t *testing.T) {
	o := NewOpenAI()
	stream := []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"hi"}}` + "\n" +
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":2}}`)
	if got := o.RuntimeError(stream, 0, false); got != "" {
		t.Errorf("valid stream = %q, want gradable", got)
	}
	if got := o.RuntimeError([]byte(""), 1, false); got == "" {
		t.Error("empty stdout must be a runtime error")
	}
	if got := o.RuntimeError([]byte(`{"type":"error","message":"auth"}`), 1, false); got == "" {
		t.Error("no agent_message + non-zero exit must be a runtime error")
	}
}

func TestScanLineOpenAI(t *testing.T) {
	o := NewOpenAI()
	tests := []struct {
		name     string
		line     string
		skill    string
		wantHit  bool
		wantNote string
	}{
		{
			name:    "path mention",
			line:    `{"type":"item.completed","item":{"type":"command_execution","command":"cat .agents/skills/go-testing/SKILL.md"}}`,
			skill:   "go-testing",
			wantHit: true,
		},
		{
			name:    "unrelated line",
			line:    `{"type":"turn.completed","usage":{"input_tokens":10}}`,
			skill:   "go-testing",
			wantHit: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit, note := o.ScanLine([]byte(tt.line), tt.skill)
			if hit != tt.wantHit {
				t.Errorf("hit = %v, want %v", hit, tt.wantHit)
			}
			if note != tt.wantNote {
				t.Errorf("note = %q, want %q", note, tt.wantNote)
			}
		})
	}
}

func TestParseEvalOutputOpenAI(t *testing.T) {
	stdout := `{"type":"item.completed","item":{"type":"agent_message","text":"first"}}
{"type":"item.completed","item":{"type":"reasoning","text":"hidden"}}
{"type":"item.completed","item":{"type":"agent_message","text":"second"}}
{"type":"turn.completed","usage":{"input_tokens":1000,"output_tokens":50}}`
	text, usage := NewOpenAI().ParseEvalOutput([]byte(stdout))
	if text != "first\nsecond" {
		t.Errorf("text = %q, want %q", text, "first\nsecond")
	}
	// No cached_input_tokens: the whole prompt is fresh input.
	if usage == nil || *usage.InputTokens != 1000 || usage.CacheReadTokens != nil ||
		*usage.OutputTokens != 50 || usage.CostUSD != nil {
		t.Errorf("usage = %+v, want input=1000 cacheRead=nil output=50 cost=nil", usage)
	}

	// codex reports input_tokens as the whole prompt with cached_input_tokens a
	// subset; the cached portion moves to CacheReadTokens and input becomes the
	// fresh remainder.
	cached := `{"type":"turn.completed","usage":{"input_tokens":103323,"cached_input_tokens":84736,"output_tokens":1294,"reasoning_output_tokens":90}}`
	_, usage = NewOpenAI().ParseEvalOutput([]byte(cached))
	if usage == nil || *usage.InputTokens != 18587 || *usage.CacheReadTokens != 84736 ||
		*usage.OutputTokens != 1294 {
		t.Errorf("usage = %+v, want input=18587 cacheRead=84736 output=1294", usage)
	}
}
