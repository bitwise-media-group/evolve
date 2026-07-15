// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"slices"
	"strings"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/model"
)

// grokJSONSuccess mirrors the headless --output-format json envelope documented
// in Grok's headless-mode guide (uncached input_tokens, separate cache reads).
const grokJSONSuccess = `{
  "text": "Done.",
  "stopReason": "EndTurn",
  "sessionId": "s1",
  "num_turns": 3,
  "usage": {
    "input_tokens": 100,
    "cache_read_input_tokens": 50,
    "output_tokens": 30,
    "total_tokens": 180
  },
  "total_cost_usd": 0.0123
}`

const grokJSONError = `{"type":"error","message":"Couldn't start session: auth failed"}`

func TestGrokParseEvalOutput(t *testing.T) {
	g := NewGrok()
	text, usage := g.ParseEvalOutput([]byte(grokJSONSuccess))
	if text != "Done." {
		t.Errorf("text = %q, want %q", text, "Done.")
	}
	if usage == nil {
		t.Fatal("usage = nil, want populated")
	}
	if got := derefInt(usage.InputTokens); got != 100 {
		t.Errorf("InputTokens = %d, want 100", got)
	}
	if got := derefInt(usage.CacheReadTokens); got != 50 {
		t.Errorf("CacheReadTokens = %d, want 50", got)
	}
	if got := derefInt(usage.OutputTokens); got != 30 {
		t.Errorf("OutputTokens = %d, want 30", got)
	}
	if usage.CostUSD == nil || *usage.CostUSD != 0.0123 {
		t.Errorf("CostUSD = %v, want 0.0123", usage.CostUSD)
	}

	raw := "plain text answer\n"
	if text, usage := g.ParseEvalOutput([]byte(raw)); text != raw || usage != nil {
		t.Errorf("ParseEvalOutput(plain) = (%q, %v), want (%q, nil)", text, usage, raw)
	}
}

func TestGrokRuntimeError(t *testing.T) {
	g := NewGrok()
	tests := []struct {
		name     string
		stdout   string
		exitCode int
		want     string
	}{
		{"gradable result", grokJSONSuccess, 0, ""},
		{"empty", "", 1, "empty CLI output"},
		{"plain text clean exit", "hello\n", 0, ""},
		{"plain text crash", "boom\n", 1, "unparseable CLI output"},
		{"error envelope", grokJSONError, 1, "grok run error: Couldn't start session: auth failed"},
	}
	for _, tt := range tests {
		if got := g.RuntimeError([]byte(tt.stdout), tt.exitCode, false); got != tt.want {
			t.Errorf("%s: RuntimeError = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestGrokScanLine(t *testing.T) {
	g := NewGrok()
	if hit, _ := g.ScanLine([]byte(`{"type":"text","data":"reading skills/my-skill/SKILL.md"}`), "my-skill"); !hit {
		t.Error("ScanLine(skill path) = false, want true")
	}
	if hit, _ := g.ScanLine([]byte(`{"type":"text","data":"unrelated"}`), "my-skill"); hit {
		t.Error("ScanLine(unrelated) = true, want false")
	}
}

func TestGrokTriggerSpec(t *testing.T) {
	g := NewGrok()
	spec := g.TriggerSpec("/ws", "use the skill", "grok-4.5", true)
	if !slices.Equal(spec.Argv[:3], []string{"grok", "-p", "use the skill"}) {
		t.Errorf("argv prefix = %v", spec.Argv[:3])
	}
	if !containsPair(spec.Argv, "--sandbox", "off") {
		t.Errorf("want --sandbox off when hostSandboxed: %v", spec.Argv)
	}
	if !containsPair(spec.Argv, "--output-format", "streaming-json") {
		t.Errorf("want streaming-json: %v", spec.Argv)
	}
	if !containsPair(spec.Argv, "--allow", "Skill") || !containsPair(spec.Argv, "--allow", "Read") {
		t.Errorf("want Skill+Read allows: %v", spec.Argv)
	}
	assertNoYolo(t, spec.Argv)

	spec = g.TriggerSpec("/ws", "q", "grok-4.5", false)
	if !containsPair(spec.Argv, "--sandbox", "workspace") {
		t.Errorf("want --sandbox workspace when unconfined: %v", spec.Argv)
	}
}

func TestGrokEvalSpec(t *testing.T) {
	g := NewGrok()
	spec := g.EvalSpec("/ws", model.EvalInput{
		Prompt:        "fix it",
		HostSandboxed: true,
	}, "grok-4.5")
	if !containsPair(spec.Argv, "--sandbox", "off") {
		t.Errorf("want --sandbox off: %v", spec.Argv)
	}
	if !containsPair(spec.Argv, "--max-turns", "20") {
		t.Errorf("want default max-turns 20: %v", spec.Argv)
	}
	if !containsPair(spec.Argv, "--allow", "Read") || !containsPair(spec.Argv, "--allow", "Bash(terraform *)") {
		t.Errorf("want default allow rules: %v", spec.Argv)
	}
	assertNoYolo(t, spec.Argv)

	spec = g.EvalSpec("/ws", model.EvalInput{
		Prompt:        "x",
		MaxTurns:      5,
		AllowedTools:  "Read Grep",
		HostSandboxed: false,
	}, "grok-4.5")
	if !containsPair(spec.Argv, "--sandbox", "workspace") {
		t.Errorf("want workspace: %v", spec.Argv)
	}
	if !containsPair(spec.Argv, "--max-turns", "5") {
		t.Errorf("want max-turns 5: %v", spec.Argv)
	}
	// Case override replaces the default set entirely.
	if containsPair(spec.Argv, "--allow", "Write") {
		t.Errorf("did not expect Write allow with override: %v", spec.Argv)
	}
	if !containsPair(spec.Argv, "--allow", "Grep") {
		t.Errorf("want Grep allow: %v", spec.Argv)
	}
}

func TestGrokReportsUsage(t *testing.T) {
	if !NewGrok().ReportsUsage() {
		t.Error("ReportsUsage = false, want true")
	}
}

func containsPair(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}

func assertNoYolo(t *testing.T, argv []string) {
	t.Helper()
	for _, a := range argv {
		if strings.Contains(strings.ToLower(a), "yolo") ||
			a == "--always-approve" ||
			a == "bypassPermissions" {
			t.Errorf("unexpected full-auto flag %q in %v", a, argv)
		}
	}
}
