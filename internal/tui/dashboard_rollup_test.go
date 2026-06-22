// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// TestDashboardLiveFeedback drives the dashboard through a run and checks the
// three things the redesign is about: per-case metrics roll up into the tabs, the
// detail panel shows the executing step, and finished branches collapse.
func TestDashboardLiveFeedback(t *testing.T) {
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

	// Triggers complete.
	d.apply(unitStartedMsg{ref: tr, total: 2, mode: plan.ModeRun})
	d.apply(itemDoneMsg{ref: tr, item: run.ItemResult{
		Index: 0, Label: "q1", Status: plan.StatusPass,
		Metrics: plan.ItemMetrics{Hits: new(3), Runs: new(3), AvgRunSeconds: new(9.8), InputTokens: new(1400), CostUSD: new(0.004)},
	}})
	d.apply(itemDoneMsg{ref: tr, item: run.ItemResult{
		Index: 1, Label: "q2", Status: plan.StatusPass,
		Metrics: plan.ItemMetrics{Hits: new(0), Runs: new(3), AvgRunSeconds: new(8.1), InputTokens: new(1300), CostUSD: new(0.004)},
	}})
	d.apply(unitFinishedMsg{ref: tr, sum: run.UnitSummary{Executed: true, Passed: 2, Total: 2}})

	// Eval e1 is now executing: the detail panel must show its authored spec.
	d.apply(unitStartedMsg{ref: ev, total: 2, mode: plan.ModeRun})
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
		Index: 0, Label: "e1", Status: plan.StatusPass,
		Metrics: plan.ItemMetrics{AvgRunSeconds: new(22.4), InputTokens: new(136865), OutputTokens: new(1390), CostUSD: new(0.2259), AssertPassed: new(1), AssertTotal: new(1)},
	}})
	d.apply(itemDoneMsg{ref: ev, item: run.ItemResult{
		Index: 1, Label: "e2", Status: plan.StatusFail,
		Metrics: plan.ItemMetrics{AvgRunSeconds: new(18.5), InputTokens: new(104569), OutputTokens: new(564), CostUSD: new(0.1919), AssertPassed: new(1), AssertTotal: new(2)},
	}})
	d.apply(unitFinishedMsg{ref: ev, sum: run.UnitSummary{Executed: true, Failed: 1, Passed: 1, Total: 2}})

	// Skills tab: one (skill, model) row. Eval pass rate is 1/2; the trigger
	// correctness counts runs that behaved correctly — q1 (should-trigger) fired 3/3
	// and q2 (should-not-trigger) fired 0/3, so both contribute 3 correct runs (6/6).
	d.tab = tabSkills
	rows := d.rollupRows()
	if len(rows) != 1 || rows[0].skill != "Solo" {
		t.Fatalf("skills rows = %+v, want one Solo row", rows)
	}
	if rows[0].evalPassed != 1 || rows[0].evalTotal != 2 {
		t.Errorf("eval tally = %d/%d, want 1/2", rows[0].evalPassed, rows[0].evalTotal)
	}
	if rows[0].trigCorrect != 6 || rows[0].trigRuns != 6 {
		t.Errorf("trigger correctness = %d/%d, want 6/6", rows[0].trigCorrect, rows[0].trigRuns)
	}
	if !rows[0].hasCost || rows[0].cost <= 0 {
		t.Errorf("row cost not aggregated: %+v", rows[0])
	}

	// With no prior run seeded there is nothing to compare, so improvements and
	// regressions are both empty.
	d.tab = tabImprovements
	if r := d.rollupRows(); len(r) != 0 {
		t.Errorf("improvements without a prior run = %+v, want none", r)
	}
	d.tab = tabRegressions
	if r := d.rollupRows(); len(r) != 0 {
		t.Errorf("regressions without a prior run = %+v, want none", r)
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

// TestRollupImprovementsBaselineBasis drives an eval to completion with a seeded
// without-skill baseline and checks the row ranks as an improvement, measured
// against the baseline (no previous run), with the baseline marker rendered.
func TestRollupImprovementsBaselineBasis(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	key := m1.Key()
	ev := plan.UnitRef{Skill: "solo-skill", Key: key, Kind: plan.KindEvals}
	filter := &plan.Filter{
		Skills: map[string]bool{"solo-skill": true},
		Evals:  map[string]map[string]bool{"solo-skill": {"e1": true, "e2": true}},
	}
	d := dashFromFilter(cat, []provider.Selection{m1}, filter, plan.PriorMetrics{})
	d.w, d.h = 120, 40

	// Without the skill both evals failed; with it, e1 passes (1/2).
	d.liveBaseline[caseKey{ev, "e1"}] = results.EvalCaseMetrics{Passed: new(false), PassRate: new(0.0)}
	d.liveBaseline[caseKey{ev, "e2"}] = results.EvalCaseMetrics{Passed: new(false), PassRate: new(0.0)}
	d.apply(unitStartedMsg{ref: ev, total: 2, mode: plan.ModeRun})
	d.apply(itemDoneMsg{ref: ev, item: run.ItemResult{Index: 0, Label: "e1", Status: plan.StatusPass,
		Metrics: plan.ItemMetrics{AssertPassed: new(1), AssertTotal: new(1)}}})
	d.apply(itemDoneMsg{ref: ev, item: run.ItemResult{Index: 1, Label: "e2", Status: plan.StatusFail,
		Metrics: plan.ItemMetrics{AssertPassed: new(0), AssertTotal: new(1)}}})
	d.apply(unitFinishedMsg{ref: ev, sum: run.UnitSummary{Executed: true, Passed: 1, Failed: 1, Total: 2}})

	d.tab = tabImprovements
	rows := d.rollupRows()
	if len(rows) != 1 {
		t.Fatalf("improvements = %+v, want one row", rows)
	}
	if rows[0].evalBasis != basisBaseline {
		t.Errorf("basis = %v, want baseline (no previous run)", rows[0].evalBasis)
	}
	if rows[0].evalDelta.Rate == nil || *rows[0].evalDelta.Rate != 0.5 {
		t.Errorf("pass-rate delta = %v, want +0.5", rows[0].evalDelta.Rate)
	}
	if line := d.rollupLine(rows[0], 120); !strings.Contains(line, baselineMark) {
		t.Errorf("rollup line missing baseline marker:\n%s", line)
	}

	// It is an improvement, so the regressions tab is empty.
	d.tab = tabRegressions
	if r := d.rollupRows(); len(r) != 0 {
		t.Errorf("regressions = %+v, want none", r)
	}
}
