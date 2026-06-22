// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// sharedSelDashboard drives a single model to a live path — triggers q1/q2 done,
// e1 done, e2 in flight — so execLog is [q1,q2,e1,e2] with follow on (sel = e2).
func sharedSelDashboard(t *testing.T) dashboardModel {
	t.Helper()
	cat := soloCatalog(t)
	_, m1 := soloModels()
	key := m1.Key()
	tr := plan.UnitRef{Skill: "solo-skill", Key: key, Kind: plan.KindTriggers}
	ev := plan.UnitRef{Skill: "solo-skill", Key: key, Kind: plan.KindEvals}
	filter := &plan.Filter{
		Skills:   map[string]bool{"solo-skill": true},
		Triggers: map[string]map[string]bool{"solo-skill": {"q1": true, "q2": true}},
		Evals:    map[string]map[string]bool{"solo-skill": {"e1": true, "e2": true}},
	}
	d := dashFromFilter(cat, []provider.Selection{m1}, filter, plan.PriorMetrics{})
	d.w, d.h = 120, 40
	d.apply(unitStartedMsg{ref: tr, total: 2, mode: plan.ModeRun})
	for _, q := range []string{"q1", "q2"} {
		d.apply(itemStartedMsg{ref: tr, item: run.ItemStart{Label: q}})
		d.apply(itemDoneMsg{ref: tr, item: run.ItemResult{Label: q, Status: plan.StatusPass,
			Metrics: plan.ItemMetrics{Hits: new(1), Runs: new(1)}}})
	}
	d.apply(unitFinishedMsg{ref: tr, sum: run.UnitSummary{Executed: true, Passed: 2, Total: 2}})
	d.apply(unitStartedMsg{ref: ev, total: 2, mode: plan.ModeRun})
	d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Label: "e1"}})
	d.apply(itemDoneMsg{ref: ev, item: run.ItemResult{Label: "e1", Status: plan.StatusPass,
		Output: "ANSWER", Detail: "  [PASS] e1\n",
		Metrics: plan.ItemMetrics{AvgRunSeconds: new(2.0), AssertPassed: new(1), AssertTotal: new(1)}}})
	d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Label: "e2"}})
	return d
}

// highlightLabel is the case label the unfocused Execution pane highlights.
func highlightLabel(d dashboardModel) string {
	nodes := d.buildNodeRefs()
	n := nodes[d.followHighlight(nodes)]
	if n.kind != nkCase {
		return ""
	}
	return d.units[n.unitIdx].cases[n.caseIdx].label
}

// TestSharedSelectionRunsToExecution: selecting a run in the Runs pane moves the
// Execution pane's highlight to the same case (the selection is shared state).
func TestSharedSelectionRunsToExecution(t *testing.T) {
	d := sharedSelDashboard(t)
	if got := highlightLabel(d); got != "e2" {
		t.Fatalf("while following, Execution highlight = %q, want the newest (e2)", got)
	}
	d.handleKey(runeKey("3")) // focus Runs
	d.handleKey(runeKey("k")) // select the older run (e1)
	if d.currentRun() != 2 || d.runFollow {
		t.Fatalf("Runs k: sel=%d follow=%v, want 2/false", d.currentRun(), d.runFollow)
	}
	if got := highlightLabel(d); got != "e1" {
		t.Errorf("Execution highlight = %q, want e1 (synced to the Runs selection)", got)
	}
}

// TestSharedSelectionExecutionToRuns: navigating the Execution tree moves the
// shared selection, and leaving the pane does not snap back to the newest run.
func TestSharedSelectionExecutionToRuns(t *testing.T) {
	d := sharedSelDashboard(t)
	d.handleKey(runeKey("1")) // browse, cursor seeded on e2
	d.handleKey(runeKey("k")) // up to e1
	if d.currentRun() != 2 || d.runFollow {
		t.Fatalf("browse k to e1: sel=%d follow=%v, want 2/false", d.currentRun(), d.runFollow)
	}
	// Leaving the Execution pane keeps the selection on e1 (no auto-follow jump).
	d.handleKey(runeKey("3"))
	if d.execBrowse {
		t.Error("leaving Execution should exit browse")
	}
	if d.currentRun() != 2 || d.runFollow {
		t.Errorf("after leaving Execution: sel=%d follow=%v, want 2/false (no auto-follow)", d.currentRun(), d.runFollow)
	}
}

// TestRunsEnterOpensDetails: enter in the Runs pane jumps to Details on the
// selected run, mirroring the Execution pane's enter.
func TestRunsEnterOpensDetails(t *testing.T) {
	d := sharedSelDashboard(t)
	d.handleKey(runeKey("3")) // focus Runs
	d.handleKey(runeKey("k")) // select e1
	d.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.focus != paneDetails {
		t.Errorf("enter in Runs should focus Details, got %v", d.focus)
	}
	if got := d.execLog[d.currentRun()].label; got != "e1" {
		t.Errorf("Details should track the selected run e1, got %q", got)
	}
	if !strings.Contains(d.renderDetails(90, 30), "ANSWER") {
		t.Error("Details should render the selected run's output")
	}
}

// TestRunsPreloadsPendingExecutions: the Runs pane lists every planned execution
// up front (in plan order) instead of growing as runs start, follow tracks the
// live execution rather than the last row, and a skipped unit settles its rows.
func TestRunsPreloadsPendingExecutions(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	key := m1.Key()
	tr := plan.UnitRef{Skill: "solo-skill", Key: key, Kind: plan.KindTriggers}
	ev := plan.UnitRef{Skill: "solo-skill", Key: key, Kind: plan.KindEvals}
	d := dashFromFilter(cat, []provider.Selection{m1}, nil, plan.PriorMetrics{})
	d.w, d.h = 100, 30

	// Every planned execution is listed before anything starts, in plan order.
	want := []string{"q1", "q2", "e1", "e2"}
	if len(d.execLog) != len(want) {
		t.Fatalf("execLog = %d, want %d preloaded", len(d.execLog), len(want))
	}
	for i, w := range want {
		if d.execLog[i].label != w {
			t.Errorf("execLog[%d] = %q, want %q", i, d.execLog[i].label, w)
		}
	}
	if d.currentRun() != 0 {
		t.Errorf("selection should start at the top, got %d", d.currentRun())
	}
	if out := d.renderRuns(60, 8); !strings.Contains(out, "q1") || !strings.Contains(out, "e2") {
		t.Errorf("Runs pane should list the pending executions:\n%s", out)
	}

	// Following tracks the live (most recently started) run, not the last row.
	d.apply(unitStartedMsg{ref: tr, total: 2, mode: plan.ModeRun})
	d.apply(itemStartedMsg{ref: tr, item: run.ItemStart{Label: "q2"}})
	if d.liveIdx != 1 || d.execLog[d.currentRun()].label != "q2" {
		t.Errorf("follow should track the live run q2 (idx 1): liveIdx=%d sel=%q",
			d.liveIdx, d.execLog[d.currentRun()].label)
	}

	// A skipped unit settles its preloaded rows to skipped, not perpetual pending.
	d.apply(unitSkippedMsg{ref: ev, reason: "no api key"})
	for _, c := range d.unit(ev).cases {
		if c.status != stSkipped {
			t.Errorf("skipped unit case %q = %v, want skipped", c.label, c.status)
		}
	}
}

// TestRunsFollowPause covers follow semantics: Runs tracks the newest execution,
// the Details pane pauses that follow while active, and F re-follows.
func TestRunsFollowPause(t *testing.T) {
	cat := evalOnlyCatalog(t)
	_, m1 := soloModels()
	key := m1.Key()
	ev := plan.UnitRef{Skill: "solo-skill", Key: key, Kind: plan.KindEvals}
	filter := &plan.Filter{
		Skills: map[string]bool{"solo-skill": true},
		Evals:  map[string]map[string]bool{"solo-skill": {"e1": true, "e2": true}},
	}
	d := dashFromFilter(cat, []provider.Selection{m1}, filter, plan.PriorMetrics{})
	d.w, d.h = 100, 30

	d.apply(unitStartedMsg{ref: ev, total: 2, mode: plan.ModeRun})
	d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Index: 0, Label: "e1"}})
	if d.currentRun() != 0 {
		t.Fatalf("Runs should follow the first execution, sel=%d", d.currentRun())
	}

	// Focusing Details pauses Runs' follow: a new execution does not move it.
	d.handleKey(runeKey("4"))
	d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Index: 1, Label: "e2"}})
	if d.currentRun() != 0 {
		t.Errorf("Details active should pause follow, sel=%d want 0", d.currentRun())
	}
	// Leaving Details for Runs resumes follow → snaps to the newest (index 1).
	d.handleKey(runeKey("3"))
	if d.currentRun() != 1 {
		t.Errorf("leaving Details should resume follow, sel=%d want 1", d.currentRun())
	}

	// k pauses follow; F re-follows the newest.
	d.handleKey(runeKey("k"))
	if d.runFollow {
		t.Error("k off the last row should pause follow")
	}
	d.handleKey(runeKey("F"))
	if !d.runFollow || d.currentRun() != 1 {
		t.Errorf("F should follow the newest, follow=%v sel=%d", d.runFollow, d.currentRun())
	}
}

// TestRunsPaneCentersSelection verifies the Runs window renders an odd row count
// and keeps a mid-list selection on the center row, with ▲/▼ overflow indicators
// on the outer rows.
func TestRunsPaneCentersSelection(t *testing.T) {
	cat := evalOnlyCatalog(t)
	_, m1 := soloModels()
	ev := plan.UnitRef{Skill: "solo-skill", Key: m1.Key(), Kind: plan.KindEvals}
	filter := &plan.Filter{
		Skills: map[string]bool{"solo-skill": true},
		Evals:  map[string]map[string]bool{"solo-skill": {}},
	}
	d := dashFromFilter(cat, []provider.Selection{m1}, filter, plan.PriorMetrics{})
	d.w, d.h = 120, 40 // tall enough that the Runs window hits its 7-row cap

	const n = 15
	d.apply(unitStartedMsg{ref: ev, total: n, mode: plan.ModeRun})
	for i := range n {
		d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Index: i, Label: fmt.Sprintf("e%d", i)}})
	}
	if len(d.execLog) != n {
		t.Fatalf("execLog = %d, want %d", len(d.execLog), n)
	}

	// Focus Runs, go to the oldest row, then step to a comfortably mid-list one.
	d.handleKey(runeKey("3"))
	d.handleKey(runeKey("g"))
	for range 7 {
		d.handleKey(runeKey("j"))
	}
	if sel := d.currentRun(); sel != 7 {
		t.Fatalf("selection = %d, want 7 (mid-list)", sel)
	}

	w, _, runsH, _ := d.rightDims()
	h := runsH - 2
	if h%2 == 0 {
		t.Fatalf("Runs content height = %d, want odd", h)
	}

	rows := strings.Split(d.renderRuns(w, h), "\n")
	if len(rows) != h {
		t.Fatalf("rendered %d rows, want %d", len(rows), h)
	}

	selRow := -1
	for i, r := range rows {
		if strings.Contains(r, "›") { // the selected gutter glyph
			selRow = i
		}
	}
	if selRow != h/2 {
		t.Errorf("selected row at index %d, want center %d:\n%s", selRow, h/2, strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[0], "▲") {
		t.Errorf("top row should be the ▲ above indicator, got %q", rows[0])
	}
	if !strings.Contains(rows[h-1], "▼") {
		t.Errorf("bottom row should be the ▼ below indicator, got %q", rows[h-1])
	}
}
