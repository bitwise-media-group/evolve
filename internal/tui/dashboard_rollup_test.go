// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/run"
)

// TestDashboardLiveFeedback drives the dashboard through a run and checks the
// three things the redesign is about: per-case metrics roll up into the tabs, the
// detail panel shows the executing step, and finished branches collapse.
func TestDashboardLiveFeedback(t *testing.T) {
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

	// Triggers complete.
	d.apply(unitStartedMsg{ref: tr, total: 2, mode: run.ModeRun})
	d.apply(itemDoneMsg{ref: tr, item: run.ItemResult{
		Index: 0, Label: "q1", Status: run.StatusPass,
		Metrics: run.ItemMetrics{Hits: new(3), Runs: new(3), AvgRunSeconds: new(9.8), InputTokens: new(1400), CostUSD: new(0.004)},
	}})
	d.apply(itemDoneMsg{ref: tr, item: run.ItemResult{
		Index: 1, Label: "q2", Status: run.StatusPass,
		Metrics: run.ItemMetrics{Hits: new(0), Runs: new(3), AvgRunSeconds: new(8.1), InputTokens: new(1300), CostUSD: new(0.004)},
	}})
	d.apply(unitFinishedMsg{ref: tr, sum: run.UnitSummary{Executed: true, Passed: 2, Total: 2}})

	// Eval e1 is now executing: the detail panel must show its authored spec.
	d.apply(unitStartedMsg{ref: ev, total: 2, mode: run.ModeRun})
	d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Index: 0, Label: "e1"}})
	nodes := d.buildNodeRefs()
	if caseNodes(nodes) == 0 {
		t.Fatal("active model should expand to case rows mid-run")
	}
	detail := d.renderDetails(90, 40)
	for _, want := range []string{"e1", "Prompt", "do the thing", "Files"} {
		if !strings.Contains(detail, want) {
			t.Errorf("executing-step detail missing %q:\n%s", want, detail)
		}
	}
	if len(d.inflight) != 1 {
		t.Errorf("inflight = %d, want 1", len(d.inflight))
	}

	// Evals complete.
	d.apply(itemDoneMsg{ref: ev, item: run.ItemResult{
		Index: 0, Label: "e1", Status: run.StatusPass,
		Metrics: run.ItemMetrics{AvgRunSeconds: new(22.4), InputTokens: new(136865), OutputTokens: new(1390), CostUSD: new(0.2259), AssertPassed: new(1), AssertTotal: new(1)},
	}})
	d.apply(itemDoneMsg{ref: ev, item: run.ItemResult{
		Index: 1, Label: "e2", Status: run.StatusFail,
		Metrics: run.ItemMetrics{AvgRunSeconds: new(18.5), InputTokens: new(104569), OutputTokens: new(564), CostUSD: new(0.1919), AssertPassed: new(1), AssertTotal: new(2)},
	}})
	d.apply(unitFinishedMsg{ref: ev, sum: run.UnitSummary{Executed: true, Failed: 1, Passed: 1, Total: 2}})

	// Summary rollup: 3 of 4 cases passed, cost and tokens summed.
	d.tab = tabSummary
	rows := d.tabRows()
	if len(rows) != 3 || rows[0].title != "Overall" {
		t.Fatalf("summary rows = %+v", rows)
	}
	if rows[0].passed != 3 || rows[0].total != 4 {
		t.Errorf("overall = %d/%d, want 3/4", rows[0].passed, rows[0].total)
	}
	wantIn := 1400 + 1300 + 136865 + 104569
	if rows[0].in != wantIn {
		t.Errorf("overall input tokens = %d, want %d", rows[0].in, wantIn)
	}
	if !rows[0].hasCost || rows[0].cost <= 0 {
		t.Errorf("overall cost not aggregated: %+v", rows[0])
	}
	// triggers row: 2/2; evals row: 1/2.
	if rows[1].passed != 2 || rows[2].passed != 1 || rows[2].total != 2 {
		t.Errorf("tier rows = triggers %d/%d, evals %d/%d", rows[1].passed, rows[1].total, rows[2].passed, rows[2].total)
	}

	// Skills grouping yields one row for solo-skill.
	d.tab = tabSkills
	if sr := d.tabRows(); len(sr) != 1 || sr[0].title != "solo-skill" {
		t.Errorf("skills rows = %+v, want one solo-skill row", sr)
	}

	// Everything is done, so the plugin collapses to a single row with no cases.
	nodes = d.buildNodeRefs()
	if caseNodes(nodes) != 0 {
		t.Errorf("completed plugin should collapse; still has %d case rows", caseNodes(nodes))
	}
	if len(nodes) != 1 || nodes[0].kind != nkPlugin || !nodes[0].collapsed {
		t.Errorf("nodes after completion = %+v, want one collapsed plugin", nodes)
	}
}
