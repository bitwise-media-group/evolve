// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// TestCaseMetricCellsGatedOnCompletion proves the execution pane resolves a delta
// basis (and so colors the metrics) only once a case is complete — never while it
// is still running, so the row does not flicker as work finishes underneath.
func TestCaseMetricCellsGatedOnCompletion(t *testing.T) {
	ev := run.UnitRef{Skill: "s", Key: "fake/m1", Kind: run.KindEvals}
	d := dashboardModel{prior: run.PriorMetrics{}, liveBaseline: map[caseKey]results.EvalCaseMetrics{
		{ev, "e1"}: {PassRate: new(0.0)},
	}}
	metrics := run.ItemMetrics{AssertPassed: new(1), AssertTotal: new(1)}

	running := &caseState{kind: run.KindEvals, label: "e1", status: stRunning, metrics: metrics}
	if _, basis := d.caseMetricCells(ev, running); basis != basisNone {
		t.Errorf("running case basis = %v, want none (no delta until complete)", basis)
	}
	done := &caseState{kind: run.KindEvals, label: "e1", status: stPass, liveDone: true, metrics: metrics}
	if _, basis := d.caseMetricCells(ev, done); basis != basisBaseline {
		t.Errorf("completed case basis = %v, want baseline", basis)
	}
}

// TestExecutingPaneAndRuler covers the redesign: a ruler splits the active
// model's trigger and eval rows in the left pane, and the Executing pane is a
// navigable log of executions showing the selected one's output, verdict, and
// open hints.
func TestExecutingPaneAndRuler(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	key := m1.Key()
	tr := run.UnitRef{Skill: "solo-skill", Key: key, Kind: run.KindTriggers}
	ev := run.UnitRef{Skill: "solo-skill", Key: key, Kind: run.KindEvals}
	plan := []run.UnitRef{tr, ev}
	filter := &run.Filter{
		Skills:   map[string]bool{"solo-skill": true},
		Triggers: map[string]map[string]bool{"solo-skill": {"q1": true, "q2": true}},
		Evals:    map[string]map[string]bool{"solo-skill": {"e1": true, "e2": true}},
	}
	d := newDashboard(cat, plan, filter, run.PriorMetrics{})
	d.w, d.h = 120, 40

	// Triggers running, evals still pending: the active model expands and exactly
	// one ruler sits between its last trigger row and its first eval row.
	d.apply(unitStartedMsg{ref: tr, total: 2, mode: run.ModeRun})
	d.apply(itemStartedMsg{ref: tr, item: run.ItemStart{Index: 0, Label: "q1"}})

	nodes := d.buildNodeRefs()
	rules, ruleIdx, lastTrig, firstEval := 0, -1, -1, -1
	for i, n := range nodes {
		switch {
		case n.kind == nkRule:
			rules++
			ruleIdx = i
		case n.kind == nkCase && d.units[n.unitIdx].ref.Kind == run.KindTriggers:
			lastTrig = i
		case n.kind == nkCase && firstEval == -1:
			firstEval = i
		}
	}
	if rules != 1 {
		t.Fatalf("want exactly one ruler between tiers, got %d in %+v", rules, nodes)
	}
	if lastTrig >= ruleIdx || ruleIdx >= firstEval {
		t.Errorf("ruler at %d not between last trigger %d and first eval %d", ruleIdx, lastTrig, firstEval)
	}
	if left := d.renderLeft(nodes, d.followHighlight(nodes), 80, 20); !strings.Contains(left, "─") {
		t.Errorf("left pane missing ruler glyph:\n%s", left)
	}

	// Finish the trigger, then start and finish an eval carrying output, a verdict,
	// and retained paths. Runs auto-follows the live execution and shows its
	// output, verdict, and o/l open hints.
	d.apply(itemDoneMsg{ref: tr, item: run.ItemResult{
		Index: 0, Label: "q1", Status: run.StatusPass,
		Metrics: run.ItemMetrics{Hits: new(1), Runs: new(1)},
	}})
	d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Index: 0, Label: "e1"}})
	d.apply(itemDoneMsg{ref: ev, item: run.ItemResult{
		Index: 0, Label: "e1", Status: run.StatusPass,
		Output:        "FINAL ANSWER LINE",
		Detail:        "  [PASS] e1: output matches /ok/\n",
		WorkspacePath: "/tmp/evolve-run.x/evals.abc",
		LogPath:       "/tmp/evolve-run.x/evals.abc.log",
		Metrics:       run.ItemMetrics{AvgRunSeconds: new(3.0), AssertPassed: new(1), AssertTotal: new(1)},
	}})

	// execLog preloads every planned execution (q1, q2, e1, e2); Runs follows the
	// live one, so e1 (most recently started) is selected.
	if len(d.execLog) != 4 {
		t.Fatalf("execLog=%d, want 4 preloaded executions", len(d.execLog))
	}
	if got := d.execLog[d.currentRun()].label; got != "e1" {
		t.Fatalf("selected run = %q, want the live execution e1", got)
	}
	detail := d.renderDetails(90, 30)
	for _, want := range []string{"FINAL ANSWER LINE", "Verdict", "output matches", "[o] open dir", "[l] open log"} {
		if !strings.Contains(detail, want) {
			t.Errorf("Details pane missing %q:\n%s", want, detail)
		}
	}

	// k (in the Runs pane, focused by default) selects the previous row and pauses
	// follow; G jumps to the bottom of the list; f re-follows the live execution.
	d.handleKey(runeKey("k"))
	if d.runFollow || d.execLog[d.currentRun()].label != "q2" {
		t.Errorf("k should select the previous run: follow=%v sel=%q", d.runFollow, d.execLog[d.currentRun()].label)
	}
	d.handleKey(runeKey("G"))
	if d.currentRun() != len(d.execLog)-1 {
		t.Errorf("G should jump to the bottom: sel=%d want %d", d.currentRun(), len(d.execLog)-1)
	}
	d.handleKey(runeKey("f"))
	if !d.runFollow || d.execLog[d.currentRun()].label != "e1" {
		t.Errorf("f should re-follow the live execution: follow=%v sel=%q", d.runFollow, d.execLog[d.currentRun()].label)
	}
}

// TestExecutionRenderIndependentOfFocus guards the disappearing-nodes fix: the
// Execution pane renders the same whether or not it is focused — leaving the pane
// no longer collapses the view onto just the highlighted model's subtree (which
// blanked every other plugin/model and left dead space below).
func TestExecutionRenderIndependentOfFocus(t *testing.T) {
	cat := soloCatalog(t)
	sels, _ := soloModels()
	key := func(i int) string { return sels[i].Key() }
	ref := func(i int, k run.Kind) run.UnitRef {
		return run.UnitRef{Skill: "solo-skill", Key: key(i), Kind: k}
	}
	plan := []run.UnitRef{
		ref(0, run.KindTriggers), ref(0, run.KindEvals),
		ref(1, run.KindTriggers), ref(1, run.KindEvals),
	}
	d := newDashboard(cat, plan, nil, run.PriorMetrics{})
	d.w, d.h = 80, 40

	nodes := d.buildNodeRefsWith(func(nodeKey) bool { return true }) // every group open
	const w, h = 60, 7
	if len(nodes) <= h {
		t.Fatalf("need an overflowing tree, got %d nodes", len(nodes))
	}
	hl := -1
	for i, n := range nodes { // a case in the first model
		if n.kind == nkCase && n.mi == 0 {
			hl = i
			break
		}
	}
	if hl < 0 {
		t.Fatal("no case row in the first model")
	}

	d.execBrowse = true
	browse := d.renderLeftBody(nodes, hl, w, h)
	d.execBrowse = false
	follow := d.renderLeftBody(nodes, hl, w, h)
	if browse != follow {
		t.Errorf("Execution render depends on focus (nodes vanish on blur):\n--browse--\n%s\n--follow--\n%s", browse, follow)
	}
	if got := len(strings.Split(follow, "\n")); got != h {
		t.Errorf("rendered %d rows, want %d (a full pane, no blank gap)", got, h)
	}

	// Every model stays reachable: highlighting the second model brings its row
	// on-screen (the old pin path only ever showed the highlighted model).
	m2 := -1
	for i, n := range nodes {
		if n.kind == nkModel && n.mi == 1 {
			m2 = i
		}
	}
	if m2 < 0 {
		t.Fatal("second model node not built")
	}
	if !strings.Contains(d.renderLeftBody(nodes, m2, w, h), key(1)) {
		t.Errorf("second model %q not shown even when highlighted", key(1))
	}
}

// TestExecutionBrowseKeepsCursorOnScreen pins the navigation fix: while browsing
// an overflowing tree the highlight must stay on-screen at the top, middle, and
// bottom of the list — the old renderer pinned the live model and let the cursor
// scroll off above and below.
func TestExecutionBrowseKeepsCursorOnScreen(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	key := m1.Key()
	tr := run.UnitRef{Skill: "solo-skill", Key: key, Kind: run.KindTriggers}
	ev := run.UnitRef{Skill: "solo-skill", Key: key, Kind: run.KindEvals}
	filter := &run.Filter{
		Skills:   map[string]bool{"solo-skill": true},
		Triggers: map[string]map[string]bool{"solo-skill": {"q1": true, "q2": true}},
		Evals:    map[string]map[string]bool{"solo-skill": {"e1": true, "e2": true}},
	}
	d := newDashboard(cat, []run.UnitRef{tr, ev}, filter, run.PriorMetrics{})
	d.w, d.h = 120, 40

	// A live path (so the model is expanded), then focus the Execution pane.
	d.apply(unitStartedMsg{ref: tr, total: 2, mode: run.ModeRun})
	d.apply(itemStartedMsg{ref: tr, item: run.ItemStart{Label: "q1"}})
	d.handleKey(runeKey("1"))

	const w, h = 60, 5
	if nodes := d.execNodes(); len(nodes) <= h {
		t.Fatalf("need an overflowing tree, got %d nodes for h=%d", len(nodes), h)
	}
	// Walk to the top (plugin row), step down through the headers, jump to the
	// bottom, and back up; the cursor glyph must appear in every rendering.
	for _, k := range []string{"g", "j", "j", "G", "k"} {
		d.handleKey(runeKey(k))
		body := d.renderLeftBody(d.execNodes(), d.execSel, w, h)
		if !strings.Contains(body, "›") {
			t.Errorf("after %q the highlight ran off-screen (no › in body):\n%s", k, body)
		}
	}
}

// TestExecutionBrowseMode covers the navigable tree: focusing the Execution pane
// enters browse mode seeded from the live path, the cursor never lands on a
// ruler, opening a completed case selects its Runs row and shows it in Details,
// and leaving the pane discards the browse state (reverting to auto-follow).
func TestExecutionBrowseMode(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	key := m1.Key()
	tr := run.UnitRef{Skill: "solo-skill", Key: key, Kind: run.KindTriggers}
	ev := run.UnitRef{Skill: "solo-skill", Key: key, Kind: run.KindEvals}
	filter := &run.Filter{
		Skills:   map[string]bool{"solo-skill": true},
		Triggers: map[string]map[string]bool{"solo-skill": {"q1": true, "q2": true}},
		Evals:    map[string]map[string]bool{"solo-skill": {"e1": true, "e2": true}},
	}
	d := newDashboard(cat, []run.UnitRef{tr, ev}, filter, run.PriorMetrics{})
	d.w, d.h = 120, 40

	// Drive triggers to completion, finish e1, and leave e2 in flight — a live
	// path with both a settled case (e1) and a running one (e2).
	d.apply(unitStartedMsg{ref: tr, total: 2, mode: run.ModeRun})
	for _, q := range []string{"q1", "q2"} {
		d.apply(itemStartedMsg{ref: tr, item: run.ItemStart{Label: q}})
		d.apply(itemDoneMsg{ref: tr, item: run.ItemResult{Label: q, Status: run.StatusPass,
			Metrics: run.ItemMetrics{Hits: new(1), Runs: new(1)}}})
	}
	d.apply(unitFinishedMsg{ref: tr, sum: run.UnitSummary{Executed: true, Passed: 2, Total: 2}})
	d.apply(unitStartedMsg{ref: ev, total: 2, mode: run.ModeRun})
	d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Label: "e1"}})
	d.apply(itemDoneMsg{ref: ev, item: run.ItemResult{Label: "e1", Status: run.StatusPass,
		Output: "ANSWER", Detail: "  [PASS] e1\n",
		Metrics: run.ItemMetrics{AvgRunSeconds: new(2.0), AssertPassed: new(1), AssertTotal: new(1)}}})
	d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Label: "e2"}})

	// Focus the Execution pane → browse mode, seeded from the live path so the
	// running model is already expanded.
	d.handleKey(runeKey("1"))
	if !d.execBrowse {
		t.Fatal("focusing the Execution pane should enter browse mode")
	}
	if !d.execExpand[nodeKey{kind: nkModel}] {
		t.Fatalf("browse should seed the live model expanded: %+v", d.execExpand)
	}

	// Walking the cursor never settles on a ruler divider.
	for range 12 {
		d.handleKey(runeKey("k"))
		nodes := d.execNodes()
		if nodes[d.execSel].kind == nkRule {
			t.Fatalf("cursor landed on a ruler at index %d", d.execSel)
		}
	}

	// Put the cursor on the finished e1 case and open it: focus moves to Details,
	// the Runs selection lands on e1, and Details shows its output.
	nodes := d.execNodes()
	e1 := -1
	for i, n := range nodes {
		if n.kind == nkCase && d.units[n.unitIdx].cases[n.caseIdx].label == "e1" {
			e1 = i
		}
	}
	if e1 < 0 {
		t.Fatal("e1 case row not visible in the expanded tree")
	}
	d.execSel = e1
	d.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.focus != paneDetails {
		t.Errorf("opening a case should focus Details, got %v", d.focus)
	}
	if got := d.execLog[d.currentRun()].label; got != "e1" {
		t.Errorf("Runs selection = %q, want e1", got)
	}
	if !strings.Contains(d.renderDetails(90, 30), "ANSWER") {
		t.Error("Details should show the opened case's output")
	}
	// Leaving the Execution pane discarded the browse state.
	if d.execBrowse || d.execExpand != nil {
		t.Errorf("leaving the Execution pane should clear browse state: browse=%v expand=%v", d.execBrowse, d.execExpand)
	}
}
