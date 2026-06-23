// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"testing"

	"github.com/bitwise-media-group/evolve/internal/model"
)

func TestAllAndByID(t *testing.T) {
	all := All()
	if len(all) != 6 {
		t.Fatalf("All() = %d harnesses, want 6", len(all))
	}
	for _, id := range []string{
		model.HarnessClaude, model.HarnessCodex, model.HarnessGemini,
		model.HarnessCursor, model.HarnessCopilot, model.HarnessAntigravity,
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
// invocations) implement it; the envelope/text harnesses and Gemini do not, so
// a tool_call assertion against them is skipped.
func TestToolCallReporterCapability(t *testing.T) {
	want := map[string]bool{
		model.HarnessClaude: true, model.HarnessCodex: true, model.HarnessGemini: false,
		model.HarnessCursor: false, model.HarnessCopilot: false, model.HarnessAntigravity: false,
	}
	for _, h := range All() {
		_, isReporter := h.(ToolCallReporter)
		if isReporter != want[h.ID()] {
			t.Errorf("%s ToolCallReporter = %v, want %v", h.ID(), isReporter, want[h.ID()])
		}
	}
}
