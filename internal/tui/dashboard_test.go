// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// TestDashboardSeedsPriorAndQueuedCases pins the partial-rerun display: every
// on-disk case shows, the queued case is pending and counted, a case that passed
// last run is shown from its stored result (prior, excluded from progress), and a
// case with no stored result and nothing queued reads as no-data.
func TestDashboardSeedsPriorAndQueuedCases(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()

	// Commit a prior run where q1 and e1 passed; q2 and e2 have no stored result.
	f := &results.File{Schema: results.Schema, Plugin: "solo", Skill: "solo-skill"}
	f.SetTrigger(m1.Key(), &results.TriggerEntry{
		Header:  results.Header{Provider: "fake", Model: "m1", Executed: true},
		Results: []results.TriggerResult{{Query: "q1", ShouldTrigger: true, Hits: new(3), Runs: new(3), Passed: new(true), AvgRunSeconds: new(1.5)}},
		Summary: results.TriggerSummary{Total: 1},
	})
	f.SetEval(m1.Key(), &results.EvalEntry{
		Header:  results.Header{Provider: "fake", Model: "m1", Executed: true},
		Results: []results.EvalResult{{ID: "e1", Passed: new(true), Summary: &results.GradeSummary{Passed: 2, Total: 3, PassRate: new(1.0)}}},
		Summary: results.EvalSummary{Passed: new(1), Total: 1},
	})
	if _, err := f.SaveDir(cat[0].ResultsDir, "json"); err != nil {
		t.Fatal(err)
	}
	prior := plan.LoadPriorMetrics(cat)

	// Only q2 is queued this run (the --failed-style rerun set).
	filter := &plan.Filter{
		Skills:   map[string]bool{"solo-skill": true},
		Triggers: map[string]map[string]bool{"solo-skill": {"q2": true}},
	}
	d := dashFromFilter(cat, []harness.Selection{m1}, filter, prior)

	tr := plan.UnitRef{Skill: "solo-skill", Key: m1.Key(), Kind: plan.KindTriggers}
	ev := plan.UnitRef{Skill: "solo-skill", Key: m1.Key(), Kind: plan.KindEvals}
	trCases, evCases := d.unit(tr).byLabel, d.unit(ev).byLabel

	if c := trCases["q2"]; c == nil || c.status != stPending || c.prior {
		t.Errorf("q2 = %+v, want pending non-prior (queued)", c)
	}
	if c := trCases["q1"]; c == nil || c.status != stPass || !c.prior {
		t.Errorf("q1 = %+v, want prior pass", c)
	}
	if c := evCases["e1"]; c == nil || c.status != stPass || !c.prior {
		t.Errorf("e1 = %+v, want prior pass", c)
	} else if intOr0(c.metrics.AssertPassed) != 2 || intOr0(c.metrics.AssertTotal) != 3 {
		t.Errorf("e1 assertion counts = %v/%v, want 2/3 (from the stored grade summary)", c.metrics.AssertPassed, c.metrics.AssertTotal)
	}
	if c := evCases["e2"]; c == nil || c.status != stNoData || !c.prior {
		t.Errorf("e2 = %+v, want prior no-data", c)
	}

	// The evals unit has nothing queued, so it settles from its prior cases (e1
	// passed) rather than reading "pending".
	if u := d.unit(ev); u.status != stPass {
		t.Errorf("all-prior evals unit status = %v, want pass (settled from prior)", u.status)
	}

	// Progress counts only the queued case, not the three prior rows.
	if _, _, _, total, _ := d.overallProgress(); total != 1 {
		t.Errorf("progress total = %d, want 1 (only the queued case)", total)
	}

	// The tree renders every on-disk case, with the no-data glyph for e2.
	d.w, d.h = 120, 40
	nodes := d.buildNodeRefsWith(func(nodeKey) bool { return true }) // every group open
	tree := d.renderLeftBody(nodes, 0, 110, len(nodes)+4)
	for _, want := range []string{"q1", "q2", "e1", "e2", "·"} {
		if !strings.Contains(tree, want) {
			t.Errorf("execution tree missing %q:\n%s", want, tree)
		}
	}
}

// TestQueuedCaseShowsPriorUntilLive pins request: a queued case displays its prior
// result and counts as pending until its live result lands, then updates.
func TestQueuedCaseShowsPriorUntilLive(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	f := &results.File{Schema: results.Schema, Plugin: "solo", Skill: "solo-skill"}
	f.SetTrigger(m1.Key(), &results.TriggerEntry{
		Header:  results.Header{Provider: "fake", Model: "m1", Executed: true},
		Results: []results.TriggerResult{{Query: "q1", ShouldTrigger: true, Hits: new(3), Runs: new(3), Passed: new(true), AvgRunSeconds: new(1.5)}},
		Summary: results.TriggerSummary{Total: 1},
	})
	if _, err := f.SaveDir(cat[0].ResultsDir, "json"); err != nil {
		t.Fatal(err)
	}
	prior := plan.LoadPriorMetrics(cat)

	tr := plan.UnitRef{Skill: "solo-skill", Key: m1.Key(), Kind: plan.KindTriggers}
	filter := &plan.Filter{ // q1 is queued AND has a prior pass
		Skills:   map[string]bool{"solo-skill": true},
		Triggers: map[string]map[string]bool{"solo-skill": {"q1": true}},
	}
	d := dashFromFilter(cat, []harness.Selection{m1}, filter, prior)

	q1 := d.unit(tr).byLabel["q1"]
	if q1.status != stPass || q1.prior || q1.liveDone {
		t.Fatalf("q1 = %+v, want prior pass shown, queued (not prior), not yet live", q1)
	}
	if ok, _, _, total, _ := d.overallProgress(); ok != 0 || total != 1 {
		t.Errorf("queued case showing a prior pass must count as pending: ok=%d total=%d, want 0/1", ok, total)
	}

	// Its live result overwrites the prior display and settles progress.
	d.apply(unitStartedMsg{ref: tr, total: 1, mode: plan.ModeRun})
	d.apply(itemStartedMsg{ref: tr, item: run.ItemStart{Index: 0, Label: "q1"}})
	d.apply(itemDoneMsg{ref: tr, item: run.ItemResult{Index: 0, Label: "q1", Status: plan.StatusFail}})
	if !q1.liveDone || q1.status != stFail {
		t.Errorf("after live result q1 = %+v, want fresh fail + liveDone", q1)
	}
	if _, bad, _, _, _ := d.overallProgress(); bad != 1 {
		t.Errorf("after live fail, bad=%d, want 1", bad)
	}
}

// inflightCount counts the live execution timers tracked for one case.
func inflightCount(d dashboardModel, ref plan.UnitRef, label string) int {
	n := 0
	for _, ifl := range d.inflight {
		if ifl.ref == ref && ifl.label == label {
			n++
		}
	}
	return n
}

// TestBaselineRunningLifecycle pins the baseline-phase row state: a baseline start
// flags the eval's row running-with-baseline (one execution-log entry, one live
// timer), the run-under-test start clears the flag without doubling the timer, and
// completion settles the row. The yellow/blue tint is a render-only style choice.
func TestBaselineRunningLifecycle(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	ev := plan.UnitRef{Skill: "solo-skill", Key: m1.Key(), Kind: plan.KindEvals}
	filter := &plan.Filter{
		Skills: map[string]bool{"solo-skill": true},
		Evals:  map[string]map[string]bool{"solo-skill": {"e1": true}},
	}
	d := dashFromFilter(cat, []harness.Selection{m1}, filter, plan.PriorMetrics{})
	d.w, d.h = 120, 40
	d.apply(unitStartedMsg{ref: ev, total: 1, mode: plan.ModeRun})

	// Baseline starts first.
	d.apply(baselineStartedMsg{ref: ev, item: run.ItemStart{Label: "e1"}})
	cr := d.unit(ev).byLabel["e1"]
	if cr == nil || cr.status != stRunning || !cr.baselineRunning {
		t.Fatalf("after baselineStarted: %+v, want running + baselineRunning", cr)
	}
	if got := inflightCount(d, ev, "e1"); got != 1 {
		t.Errorf("inflight for e1 during baseline = %d, want 1", got)
	}
	if got := d.renderRuns(120, 10); !strings.Contains(got, "e1") {
		t.Errorf("runs pane did not render the baseline row:\n%s", got)
	}

	// The run under test starts: the baseline flag clears, no duplicate timer.
	d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Label: "e1"}})
	if cr.baselineRunning || cr.status != stRunning {
		t.Errorf("after itemStarted: baselineRunning=%v status=%v, want false + running", cr.baselineRunning, cr.status)
	}
	if got := inflightCount(d, ev, "e1"); got != 1 {
		t.Errorf("inflight for e1 after run start = %d, want 1 (no duplicate)", got)
	}

	// Completion settles the row.
	d.apply(itemDoneMsg{ref: ev, item: run.ItemResult{Label: "e1", Status: plan.StatusPass,
		Metrics: plan.ItemMetrics{AvgRunSeconds: new(2.0)}}})
	if cr.baselineRunning || cr.status != stPass {
		t.Errorf("after itemDone: baselineRunning=%v status=%v, want false + pass", cr.baselineRunning, cr.status)
	}
	if got := inflightCount(d, ev, "e1"); got != 0 {
		t.Errorf("inflight for e1 after done = %d, want 0", got)
	}
}

// TestQuitDialog covers the quit-confirmation flow.
func TestQuitDialog(t *testing.T) {
	m := testModel(t)
	m = step(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = step(m, runeKey("r"))
	if m.screen != screenDashboard {
		t.Fatal("did not reach the dashboard")
	}

	// q opens the dialog without quitting.
	m, cmd := stepCmd(m, runeKey("q"))
	if cmd != nil {
		t.Fatal("q should not quit immediately")
	}
	if !m.dash.confirmQuit || !strings.Contains(m.View().Content, "Are you sure") {
		t.Errorf("q should open the quit dialog:\n%s", m.View().Content)
	}
	// n dismisses it.
	m, _ = stepCmd(m, runeKey("n"))
	if m.dash.confirmQuit {
		t.Error("n should dismiss the quit dialog")
	}
	// q then y quits.
	m, _ = stepCmd(m, runeKey("q"))
	if _, cmd = stepCmd(m, runeKey("y")); cmd == nil {
		t.Error("y in the dialog should quit")
	}
	// Two ctrl+c in a row quit immediately.
	m, _ = stepCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.dash.confirmQuit {
		t.Error("first ctrl+c should open the dialog")
	}
	if _, cmd = stepCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
		t.Error("second ctrl+c should quit")
	}
}

// TestCaseAggStatusThresholds pins the rollup classifier: an error still wins
// outright, but a failing tier only rolls the group red below its report
// threshold — at or above it the group passes by threshold (orange) — and the
// worst tier verdict decides a mixed group.
func TestCaseAggStatusThresholds(t *testing.T) {
	d := newDashboard(plan.Plan{}, soloCatalog(t), plan.PriorMetrics{}, testThresholds)
	c := func(kind plan.Kind, st status) *caseState { return &caseState{kind: kind, status: st} }
	trig := func(st status) *caseState { return c(plan.KindTriggers, st) }
	eval := func(st status) *caseState { return c(plan.KindEvals, st) }

	cases := []struct {
		name string
		in   []*caseState
		want status
	}{
		{"all pass", []*caseState{trig(stPass), eval(stPass)}, stPass},
		{"error wins over a passing rate",
			[]*caseState{trig(stPass), trig(stPass), trig(stError)}, stError},
		{"triggers at the 50% gate",
			[]*caseState{trig(stPass), trig(stFail)}, stPassThreshold},
		{"triggers below the gate",
			[]*caseState{trig(stPass), trig(stFail), trig(stFail)}, stFail},
		{"evals above the 66% gate", // 2/3 ≈ 0.667
			[]*caseState{eval(stPass), eval(stPass), eval(stFail)}, stPassThreshold},
		{"evals below the gate", // 1/2 = 0.5
			[]*caseState{eval(stPass), eval(stFail)}, stFail},
		{"worst tier wins", // triggers 1/2 meets its gate, evals 0/1 misses its own
			[]*caseState{trig(stPass), trig(stFail), eval(stFail)}, stFail},
		{"threshold tier beats a clean tier",
			[]*caseState{trig(stPass), eval(stPass), eval(stPass), eval(stFail)}, stPassThreshold},
		{"all skipped", []*caseState{trig(stSkipped), eval(stSkipped)}, stSkipped},
		{"count-only ranks below a pass",
			[]*caseState{trig(stCount), trig(stSkipped)}, stCount},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := d.caseAggStatus(tc.in); got != tc.want {
				t.Errorf("caseAggStatus = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestUnitFinishedThresholdStatus pins the unit-level rollup: a finished unit
// with failures settles orange while its pass rate meets its tier's threshold
// and red below it, judged against the unit's own kind; errors still win.
func TestUnitFinishedThresholdStatus(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	tr := plan.UnitRef{Skill: "solo-skill", Key: m1.Key(), Kind: plan.KindTriggers}
	ev := plan.UnitRef{Skill: "solo-skill", Key: m1.Key(), Kind: plan.KindEvals}

	cases := []struct {
		name string
		ref  plan.UnitRef
		sum  run.UnitSummary
		want status
	}{
		{"triggers at the 50% gate", tr,
			run.UnitSummary{Executed: true, Passed: 1, Failed: 1, Total: 2}, stPassThreshold},
		{"triggers below the gate", tr,
			run.UnitSummary{Executed: true, Passed: 1, Failed: 2, Total: 3}, stFail},
		{"evals above the 66% gate", ev,
			run.UnitSummary{Executed: true, Passed: 2, Failed: 1, Total: 3}, stPassThreshold},
		{"evals below the gate", ev,
			run.UnitSummary{Executed: true, Passed: 1, Failed: 1, Total: 2}, stFail},
		{"all passed stays green", tr,
			run.UnitSummary{Executed: true, Passed: 2, Total: 2}, stPass},
		{"an error still wins", tr,
			run.UnitSummary{Executed: true, Passed: 1, Failed: 1, Errored: 1, Total: 3}, stError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := dashFromFilter(cat, []harness.Selection{m1}, nil, plan.PriorMetrics{})
			d.apply(unitFinishedMsg{ref: tc.ref, sum: tc.sum})
			if got := d.unit(tc.ref).status; got != tc.want {
				t.Errorf("unit status = %v, want %v", got, tc.want)
			}
		})
	}
}
