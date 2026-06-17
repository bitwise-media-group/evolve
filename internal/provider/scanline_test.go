// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import "testing"

func TestScanLine(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		line     string
		skill    string
		wantHit  bool
		wantNote string
	}{
		{
			name:     "claude Skill tool",
			provider: NewAnthropic(),
			line:     `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"go-testing"}}]}}`,
			skill:    "go-testing",
			wantHit:  true,
		},
		{
			name:     "claude Read of SKILL.md",
			provider: NewAnthropic(),
			line:     `{"message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/ws/.claude/skills/go-testing/SKILL.md"}}]}}`,
			skill:    "go-testing",
			wantHit:  true,
		},
		{
			name:     "claude Read of sibling skill",
			provider: NewAnthropic(),
			line:     `{"message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/ws/.claude/skills/go-style/SKILL.md"}}]}}`,
			skill:    "go-testing",
			wantHit:  false,
		},
		{
			name:     "claude text block mentioning skill",
			provider: NewAnthropic(),
			line:     `{"message":{"content":[{"type":"text","text":"I could use go-testing skills/go-testing/SKILL.md"}]}}`,
			skill:    "go-testing",
			wantHit:  false,
		},
		{
			name:     "codex path mention",
			provider: NewOpenAI(),
			line:     `{"type":"item.completed","item":{"type":"command_execution","command":"cat .agents/skills/go-testing/SKILL.md"}}`,
			skill:    "go-testing",
			wantHit:  true,
		},
		{
			name:     "codex unrelated line",
			provider: NewOpenAI(),
			line:     `{"type":"turn.completed","usage":{"input_tokens":10}}`,
			skill:    "go-testing",
			wantHit:  false,
		},
		{
			name:     "gemini activate_skill",
			provider: NewGoogle(),
			line:     `{"type":"tool_use","tool_name":"activate_skill","parameters":{"skill":"go-testing"}}`,
			skill:    "go-testing",
			wantHit:  true,
		},
		{
			name:     "gemini read_file fallback",
			provider: NewGoogle(),
			line:     `{"type":"tool_use","tool_name":"read_file","parameters":{"path":".gemini/skills/go-testing/SKILL.md"}}`,
			skill:    "go-testing",
			wantHit:  true,
		},
		{
			name:     "gemini error result",
			provider: NewGoogle(),
			line:     `{"type":"result","status":"error","error":{"message":"quota exceeded"}}`,
			skill:    "go-testing",
			wantHit:  false,
			wantNote: "gemini run errored; counted as no-trigger: quota exceeded",
		},
		{
			name:     "cursor readToolCall started",
			provider: NewCursor(),
			line:     `{"type":"tool_call","subtype":"started","call_id":"c1","tool_call":{"readToolCall":{"args":{"path":"/ws/.cursor/skills/go-testing/SKILL.md"}}}}`,
			skill:    "go-testing",
			wantHit:  true,
		},
		{
			name:     "cursor assistant prose mentioning path",
			provider: NewCursor(),
			line:     `{"type":"assistant","message":{"content":[{"type":"text","text":"see skills/go-testing/SKILL.md"}]}}`,
			skill:    "go-testing",
			wantHit:  false,
		},
		{
			name:     "copilot path mention",
			provider: NewCopilot(),
			line:     `Read .copilot/skills/go-testing/SKILL.md`,
			skill:    "go-testing",
			wantHit:  true,
		},
		{
			name:     "copilot plain prose",
			provider: NewCopilot(),
			line:     `Here is how I would approach go testing.`,
			skill:    "go-testing",
			wantHit:  false,
		},
		{
			name:     "non-JSON noise",
			provider: NewAnthropic(),
			line:     `warning: something`,
			skill:    "go-testing",
			wantHit:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit, note := tt.provider.ScanLine([]byte(tt.line), tt.skill)
			if hit != tt.wantHit {
				t.Errorf("hit = %v, want %v", hit, tt.wantHit)
			}
			if note != tt.wantNote {
				t.Errorf("note = %q, want %q", note, tt.wantNote)
			}
		})
	}
}

func TestParseEvalOutputAnthropic(t *testing.T) {
	stdout := `{"result":"done","usage":{"input_tokens":10,"cache_creation_input_tokens":5,"cache_read_input_tokens":85,"output_tokens":42},"total_cost_usd":0.12}`
	text, usage := NewAnthropic().ParseEvalOutput([]byte(stdout))
	if text != "done" {
		t.Errorf("text = %q, want done", text)
	}
	if usage == nil || *usage.InputTokens != 100 || *usage.OutputTokens != 42 || *usage.CostUSD != 0.12 {
		t.Errorf("usage = %+v, want input=100 output=42 cost=0.12", usage)
	}

	text, usage = NewAnthropic().ParseEvalOutput([]byte("not json at all"))
	if text != "not json at all" || usage != nil {
		t.Errorf("unparseable: text=%q usage=%v, want raw stdout and nil", text, usage)
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
	if usage == nil || *usage.InputTokens != 1000 || *usage.OutputTokens != 50 || usage.CostUSD != nil {
		t.Errorf("usage = %+v, want tokens 1000/50 and nil cost", usage)
	}
}

func TestParseEvalOutputCursor(t *testing.T) {
	text, usage := NewCursor().ParseEvalOutput([]byte(`{"type":"result","subtype":"success","is_error":false,"result":"all done"}`))
	if text != "all done" || usage != nil {
		t.Errorf("got text=%q usage=%v, want %q and nil usage", text, usage, "all done")
	}
}

func TestParseEvalOutputCopilot(t *testing.T) {
	text, usage := NewCopilot().ParseEvalOutput([]byte("  all done\n"))
	if text != "all done" || usage != nil {
		t.Errorf("got text=%q usage=%v, want %q and nil usage", text, usage, "all done")
	}
}
