// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bitwise-media-group/evolve/internal/run"
)

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
	d := newDashboard(cat, plan, filter)
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
	if left := d.renderLeft(nodes, d.liveFocus(nodes), 80, 20); !strings.Contains(left, "─") {
		t.Errorf("left pane missing ruler glyph:\n%s", left)
	}

	// Finish the trigger, then start and finish an eval carrying output, a verdict,
	// and retained paths. The Executing pane auto-follows the newest (bottom)
	// execution and shows its output, verdict, and o/l open hints.
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

	// execLog is q1 then e1 (newest last); Runs follows so e1 is selected.
	if len(d.execLog) != 2 || d.currentRun() != 1 {
		t.Fatalf("execLog=%d sel=%d, want 2 with newest selected", len(d.execLog), d.currentRun())
	}
	detail := d.renderDetails(90, 30)
	for _, want := range []string{"FINAL ANSWER LINE", "Verdict", "output matches", "[o] open dir", "[l] open log"} {
		if !strings.Contains(detail, want) {
			t.Errorf("Details pane missing %q:\n%s", want, detail)
		}
	}

	// k (in the Runs pane, focused by default) selects the older execution and
	// pauses follow; G follows the newest again.
	d.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if d.runFollow || d.currentRun() != 0 {
		t.Errorf("k should select the older execution: follow=%v sel=%d", d.runFollow, d.currentRun())
	}
	d.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if !d.runFollow || d.currentRun() != 1 {
		t.Errorf("G should follow the newest: follow=%v sel=%d", d.runFollow, d.currentRun())
	}
}
