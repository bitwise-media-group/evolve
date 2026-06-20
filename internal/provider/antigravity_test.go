// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import "testing"

func TestRuntimeErrorAntigravity(t *testing.T) {
	a := NewAntigravity()
	if got := a.RuntimeError([]byte("all done\n"), 0, false); got != "" {
		t.Errorf("text answer = %q, want gradable", got)
	}
	if got := a.RuntimeError([]byte("  \n"), 1, false); got == "" {
		t.Error("empty stdout must be a runtime error")
	}
}

func TestScanLineAntigravity(t *testing.T) {
	a := NewAntigravity()
	tests := []struct {
		name     string
		line     string
		skill    string
		wantHit  bool
		wantNote string
	}{
		{
			name:    "path mention",
			line:    `Read .antigravity/skills/go-testing/SKILL.md`,
			skill:   "go-testing",
			wantHit: true,
		},
		{
			name:    "plain prose",
			line:    `Here is how I would approach go testing.`,
			skill:   "go-testing",
			wantHit: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit, note := a.ScanLine([]byte(tt.line), tt.skill)
			if hit != tt.wantHit {
				t.Errorf("hit = %v, want %v", hit, tt.wantHit)
			}
			if note != tt.wantNote {
				t.Errorf("note = %q, want %q", note, tt.wantNote)
			}
		})
	}
}

func TestParseEvalOutputAntigravity(t *testing.T) {
	text, usage := NewAntigravity().ParseEvalOutput([]byte("  all done\n"))
	if text != "all done" || usage != nil {
		t.Errorf("got text=%q usage=%v, want %q and nil usage", text, usage, "all done")
	}
}
