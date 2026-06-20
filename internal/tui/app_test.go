// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bitwise-media-group/evolve/internal/run"
)

func TestRunTransitionAndDashboard(t *testing.T) {
	m := testModel(t)
	m = step(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// RUN.
	m = step(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if m.screen != screenDashboard {
		t.Fatal("did not switch to dashboard on RUN")
	}
	// One resolved model × {triggers, evals} = 2 units, ordered triggers first.
	if len(m.dash.units) != 2 {
		t.Fatalf("dashboard units = %d, want 2", len(m.dash.units))
	}
	if m.dash.units[0].ref.Kind != run.KindTriggers || m.dash.units[1].ref.Kind != run.KindEvals {
		t.Fatalf("units not ordered triggers→evals: %+v", m.dash.units)
	}
	// The triggers unit pre-lists its applicable cases (q1, q2) so they render
	// before they run.
	if len(m.dash.units[0].cases) != 2 {
		t.Fatalf("triggers cases = %d, want 2 (q1,q2)", len(m.dash.units[0].cases))
	}

	// Drive the triggers unit to completion via the streamed events.
	ref := m.dash.units[0].ref
	m = step(m, unitStartedMsg{ref: ref, total: 2, mode: run.ModeRun})
	m = step(m, itemStartedMsg{ref: ref, item: run.ItemStart{Index: 0, Label: "q1"}})
	if len(m.dash.inflight) != 1 {
		t.Fatalf("inflight = %d, want 1 after itemStarted", len(m.dash.inflight))
	}
	m = step(m, itemDoneMsg{ref: ref, item: run.ItemResult{Index: 0, Label: "q1", Status: run.StatusPass}})
	if len(m.dash.inflight) != 0 {
		t.Errorf("inflight = %d, want 0 after itemDone", len(m.dash.inflight))
	}
	if m.dash.units[0].byLabel["q1"].status != stPass {
		t.Errorf("case q1 status = %v, want pass", m.dash.units[0].byLabel["q1"].status)
	}
	m = step(m, itemDoneMsg{ref: ref, item: run.ItemResult{Index: 1, Label: "q2", Status: run.StatusPass}})
	m = step(m, unitFinishedMsg{ref: ref, sum: run.UnitSummary{Executed: true, Passed: 2, Total: 2}, savedRel: "evals/x/results.json"})
	if m.dash.units[0].status != stPass {
		t.Errorf("unit status = %v, want pass", m.dash.units[0].status)
	}

	out := m.View()
	for _, want := range []string{"Execution", "Rollup", "Runs", "Details"} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard view missing %q:\n%s", want, out)
		}
	}

	// Tab cycles focus across the three right panes (default is Runs).
	if m.dash.focus != paneRuns {
		t.Fatalf("default focus = %v, want Runs", m.dash.focus)
	}
	m = step(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.dash.focus != paneDetails {
		t.Errorf("Tab from Runs should focus Details, got %v", m.dash.focus)
	}
	// Focus the Rollup pane (1) and switch its tabs with → only while it is active.
	m = step(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	before := m.dash.tab
	m = step(m, tea.KeyMsg{Type: tea.KeyRight})
	if m.dash.tab == before {
		t.Error("→ in the Rollup pane did not advance the tab")
	}
	_ = m.View() // must render in every focus/tab state without panic

	m = step(m, runDoneMsg{failed: false})
	if !m.dash.done {
		t.Error("dashboard not marked done")
	}
}

func TestCancelQuits(t *testing.T) {
	m := testModel(t)
	m = step(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc on form should return a quit command")
	}
	if msg := cmd(); msg == nil {
		t.Error("expected a tea.QuitMsg from cancel")
	}
}
