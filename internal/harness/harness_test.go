// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"testing"

	"github.com/bitwise-media-group/evolve/internal/model"
)

func TestAllAndByID(t *testing.T) {
	all := All()
	if len(all) != 7 {
		t.Fatalf("All() = %d harnesses, want 7", len(all))
	}
	for _, id := range []string{
		model.HarnessClaude, model.HarnessCodex, model.HarnessGemini,
		model.HarnessCursor, model.HarnessCopilot, model.HarnessAntigravity,
		model.HarnessGrok,
	} {
		h, ok := ByID(id)
		if !ok {
			t.Errorf("ByID(%q) = not found", id)
			continue
		}
		if h.ID() != id {
			t.Errorf("ByID(%q).ID() = %q", id, h.ID())
		}
	}
	if _, ok := ByID("nope"); ok {
		t.Error("ByID(nope) = found, want none")
	}
}

// TestEvalRunnerCapability pins which harnesses implement EvalRunner: Gemini
// does not (no gradable headless run), the rest do.
func TestEvalRunnerCapability(t *testing.T) {
	want := map[string]bool{
		model.HarnessClaude: true, model.HarnessCodex: true, model.HarnessGemini: false,
		model.HarnessCursor: true, model.HarnessCopilot: true, model.HarnessAntigravity: true,
		model.HarnessGrok: true,
	}
	for _, h := range All() {
		_, isRunner := h.(EvalRunner)
		if isRunner != want[h.ID()] {
			t.Errorf("%s EvalRunner = %v, want %v", h.ID(), isRunner, want[h.ID()])
		}
	}
}

// TestToolCallReporterCapability pins which harnesses can report tool calls
// from their eval output. Claude and Codex (whose output carries tool
// invocations) implement it; the envelope/text harnesses, Gemini, and Grok
// (headless streaming-json has no tool events) do not, so a tool_call assertion
// against them is skipped.
func TestToolCallReporterCapability(t *testing.T) {
	want := map[string]bool{
		model.HarnessClaude: true, model.HarnessCodex: true, model.HarnessGemini: false,
		model.HarnessCursor: false, model.HarnessCopilot: false, model.HarnessAntigravity: false,
		model.HarnessGrok: false,
	}
	for _, h := range All() {
		_, isReporter := h.(ToolCallReporter)
		if isReporter != want[h.ID()] {
			t.Errorf("%s ToolCallReporter = %v, want %v", h.ID(), isReporter, want[h.ID()])
		}
	}
}

func TestHosted(t *testing.T) {
	sel, ok := HostedSelection("ag-ui", "staging", "Support (staging)")
	if !ok {
		t.Fatal("HostedSelection(ag-ui) = false")
	}
	m := sel.Model
	if sel.Key() != "ag-ui/staging" || m.ProviderID != "ag-ui" || m.BareID() != "staging" || m.Name != "Support (staging)" ||
		m.InputUSD != nil || m.OutputUSD != nil {
		t.Errorf("model = %+v", m)
	}
	if id, _ := m.CLIModelID("ag-ui"); id != "staging" {
		t.Errorf("endpoint id = %q", id)
	}
	if !IsHosted(sel.Harness) || IsHosted(NewClaude()) {
		t.Error("IsHosted misclassifies")
	}
	if path, ok := Available(sel.Harness); !ok || path != "" {
		t.Errorf("Available(hosted) = %q, %v; want always available with no path", path, ok)
	}
	if _, ok := sel.Harness.(EvalRunner); ok {
		t.Error("a hosted harness must not be an EvalRunner (it can never judge)")
	}
	if sel.Harness.CLI() != nil || sel.Harness.SkillDirs() != nil {
		t.Error("a hosted harness has no CLI or skill dirs")
	}
	if _, ok := HostedSelection("a2a", "x", ""); ok {
		t.Error("an unsupported protocol must not yield a selection")
	}
	if sel, _ := HostedSelection("ag-ui", "local", ""); sel.Model.Name != "local" {
		t.Errorf("display defaults to the endpoint name, got %q", sel.Model.Name)
	}
}
