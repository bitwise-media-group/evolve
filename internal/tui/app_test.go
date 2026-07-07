// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/run"
)

func TestRunTransitionAndDashboard(t *testing.T) {
	m := testModel(t)
	m = step(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// RUN.
	m = step(m, runeKey("r"))
	if m.screen != screenDashboard {
		t.Fatal("did not switch to dashboard on RUN")
	}
	// One resolved model × {triggers, evals} = 2 units, ordered triggers first.
	if len(m.dash.units) != 2 {
		t.Fatalf("dashboard units = %d, want 2", len(m.dash.units))
	}
	if m.dash.units[0].ref.Kind != plan.KindTriggers || m.dash.units[1].ref.Kind != plan.KindEvals {
		t.Fatalf("units not ordered triggers→evals: %+v", m.dash.units)
	}
	// The triggers unit pre-lists its applicable cases (q1, q2) so they render
	// before they run.
	if len(m.dash.units[0].cases) != 2 {
		t.Fatalf("triggers cases = %d, want 2 (q1,q2)", len(m.dash.units[0].cases))
	}

	// Drive the triggers unit to completion via the streamed events.
	ref := m.dash.units[0].ref
	m = step(m, unitStartedMsg{ref: ref, total: 2, mode: plan.ModeRun})
	m = step(m, itemStartedMsg{ref: ref, item: run.ItemStart{Index: 0, Label: "q1"}})
	if len(m.dash.inflight) != 1 {
		t.Fatalf("inflight = %d, want 1 after itemStarted", len(m.dash.inflight))
	}
	m = step(m, itemDoneMsg{ref: ref, item: run.ItemResult{Index: 0, Label: "q1", Status: plan.StatusPass}})
	if len(m.dash.inflight) != 0 {
		t.Errorf("inflight = %d, want 0 after itemDone", len(m.dash.inflight))
	}
	if m.dash.units[0].byLabel["q1"].status != stPass {
		t.Errorf("case q1 status = %v, want pass", m.dash.units[0].byLabel["q1"].status)
	}
	m = step(m, itemDoneMsg{ref: ref, item: run.ItemResult{Index: 1, Label: "q2", Status: plan.StatusPass}})
	m = step(m, unitFinishedMsg{ref: ref, sum: run.UnitSummary{Executed: true, Passed: 2, Total: 2}, savedRel: "evals/x/results.json"})
	if m.dash.units[0].status != stPass {
		t.Errorf("unit status = %v, want pass", m.dash.units[0].status)
	}

	out := m.View().Content
	for _, want := range []string{"Execution", "Legend", "Rollup", "Runs", "Details"} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard view missing %q:\n%s", want, out)
		}
	}

	// Tab cycles focus across the three right panes (default is Runs).
	if m.dash.focus != paneRuns {
		t.Fatalf("default focus = %v, want Runs", m.dash.focus)
	}
	m = step(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.dash.focus != paneDetails {
		t.Errorf("Tab from Runs should focus Details, got %v", m.dash.focus)
	}
	// Focus the Rollup pane (2) and switch its tabs with → only while it is active.
	m = step(m, runeKey("2"))
	before := m.dash.tab
	m = step(m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.dash.tab == before {
		t.Error("→ in the Rollup pane did not advance the tab")
	}
	_ = m.View() // must render in every focus/tab state without panic

	m = step(m, runDoneMsg{failed: false})
	if !m.dash.done {
		t.Error("dashboard not marked done")
	}
}

// TestTitleBarAlignment locks the layout: the run-wide progress bar rides the
// title bar (its percentage shows there), a blank row separates it from the
// panes, and the Execution and Rollup panes start on the same row.
func TestTitleBarAlignment(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	key := m1.Key()
	tr := plan.UnitRef{Skill: "solo-skill", Key: key, Kind: plan.KindTriggers}
	filter := &plan.Filter{
		Skills:   map[string]bool{"solo-skill": true},
		Triggers: map[string]map[string]bool{"solo-skill": {"q1": true}},
		Evals:    map[string]map[string]bool{"solo-skill": {"e1": true}},
	}
	d := dashFromFilter(cat, []harness.Selection{m1}, filter, plan.PriorMetrics{})
	d.w, d.h = 140, 30
	d.apply(unitStartedMsg{ref: tr, total: 1, mode: plan.ModeRun})
	d.apply(itemStartedMsg{ref: tr, item: run.ItemStart{Label: "q1"}})

	lines := strings.Split(d.view(), "\n")
	row := func(sub string) int {
		for i, l := range lines {
			if strings.Contains(l, sub) {
				return i
			}
		}
		return -1
	}
	exec, rollup := row("[1]─Execution"), row("[2]─Rollup")
	if exec < 0 || rollup < 0 {
		t.Fatalf("panes not found: exec=%d rollup=%d", exec, rollup)
	}
	if exec != rollup {
		t.Errorf("Execution (row %d) and Rollup (row %d) should be top-aligned", exec, rollup)
	}
	if !strings.Contains(lines[0], "%") {
		t.Errorf("the progress percentage should ride the title bar (row 0):\n%s", lines[0])
	}
	if exec < 2 || strings.TrimSpace(lines[exec-1]) != "" {
		t.Errorf("a blank separator row should sit between the title bar and the panes (panes at row %d)", exec)
	}
}

func TestCancelQuits(t *testing.T) {
	m := testModel(t)
	m = step(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc on form should return a quit command")
	}
	if msg := cmd(); msg == nil {
		t.Error("expected a tea.QuitMsg from cancel")
	}
}

// TestMouseRouting pins the root model's mouse wiring: the view declares
// cell-motion mouse mode, clicks route to whichever screen is active, and
// clicking RUN advances to the dashboard exactly like the keyboard path.
func TestMouseRouting(t *testing.T) {
	m := testModel(t)
	m = step(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("View().MouseMode = %v, want cell motion", got)
	}

	// On the form, a click lands in the form model.
	l := m.form.layout()
	m = step(m, leftClick(l.tree.x0+3, l.tree.y0+1))
	if m.screen != screenForm || m.form.focus != paneTree {
		t.Fatalf("screen=%v form focus=%d, want the click to focus the form tree", m.screen, m.form.focus)
	}

	// Clicking RUN starts the run and switches screens.
	m, cmd := stepCmd(m, leftClick(l.runBtn.x0+2, l.runBtn.y0+1))
	if m.screen != screenDashboard || cmd == nil {
		t.Fatalf("screen=%v cmd=%v, want the RUN click to reach the dashboard", m.screen, cmd)
	}

	// On the dashboard, clicks route to the dashboard model.
	dl := m.dash.layout()
	m = step(m, leftClick(dl.details.x0+3, dl.details.y0+1))
	if m.dash.focus != paneDetails {
		t.Errorf("dash focus = %v, want the click to focus Details", m.dash.focus)
	}
}

// TestMouseCancelQuits: a CANCEL click on the form quits like esc.
func TestMouseCancelQuits(t *testing.T) {
	m := testModel(t)
	m = step(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	l := m.form.layout()
	_, cmd := m.Update(leftClick(l.cancelBtn.x0+2, l.cancelBtn.y0+1))
	if cmd == nil {
		t.Fatal("a CANCEL click should return a quit command")
	}
	if msg := cmd(); msg == nil {
		t.Error("expected a tea.QuitMsg from the CANCEL click")
	}
}

// TestLineShiftingMessagesForceRepaint pins the workaround for the upstream
// ultraviolet scroll-optimization bug (stale duplicate rows after a scrolled
// repaint): every message that can shift lines vertically — keys, mouse, and
// engine progress — must chain tea.ClearScreen so the renderer full-repaints
// instead of hard-scrolling, while spinner ticks and resizes stay diff-only.
func TestLineShiftingMessagesForceRepaint(t *testing.T) {
	m := testModel(t)
	m = step(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	shifting := map[string]tea.Msg{
		"form key":     runeKey("j"),
		"engine event": warnMsg{text: "x"},
	}
	m2 := step(m, runeKey("r")) // to the dashboard
	if m2.screen != screenDashboard {
		t.Fatal("did not switch to dashboard")
	}
	for name, msg := range shifting {
		if _, cmd := stepCmd(m, msg); !yieldsClearScreen(cmd) {
			t.Errorf("%s: cmd must chain tea.ClearScreen (scroll-optimization workaround)", name)
		}
	}
	if _, cmd := stepCmd(m2, runeKey("j")); !yieldsClearScreen(cmd) {
		t.Error("dashboard key: cmd must chain tea.ClearScreen (scroll-optimization workaround)")
	}

	if _, cmd := stepCmd(m2, tea.WindowSizeMsg{Width: 100, Height: 30}); yieldsClearScreen(cmd) {
		t.Error("resize already erases in the renderer; it must not chain another clear")
	}
}

// yieldsClearScreen reports whether cmd (possibly a batch) produces the message
// tea.ClearScreen produces.
func yieldsClearScreen(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if yieldsClearScreen(c) {
				return true
			}
		}
		return false
	}
	return reflect.TypeOf(msg) == reflect.TypeOf(tea.ClearScreen())
}

// yieldsQuit reports whether cmd (possibly a batch) produces tea.QuitMsg.
func yieldsQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if yieldsQuit(c) {
				return true
			}
		}
		return false
	}
	_, ok := msg.(tea.QuitMsg)
	return ok
}
