// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"testing"

	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/results"
)

func TestColorDirections(t *testing.T) {
	// A rate is good when it rises.
	if upGood(new(0.1)) != 1 || upGood(new(-0.1)) != -1 || upGood(nil) != 0 || upGood(new(0.0)) != 0 {
		t.Error("upGood scored a rate change wrong")
	}
	// Cost and time are good when they fall.
	if downGood(new(-0.1)) != 1 || downGood(new(0.1)) != -1 || downGood(nil) != 0 || downGood(new(0.0)) != 0 {
		t.Error("downGood scored a cost/time change wrong")
	}
	if downGoodInt(new(-5)) != 1 || downGoodInt(new(5)) != -1 || downGoodInt(new(0)) != 0 {
		t.Error("downGoodInt scored a token change wrong")
	}
}

func TestEvalCaseMetricsOf(t *testing.T) {
	m := evalCaseMetricsOf(stPass, plan.ItemMetrics{
		AvgRunSeconds: new(12.0), AssertPassed: new(3), AssertTotal: new(4),
		InputTokens: new(100), OutputTokens: new(10), CostUSD: new(0.5),
	})
	if m.Passed == nil || !*m.Passed {
		t.Error("a pass status should set Passed true")
	}
	if m.PassRate == nil || *m.PassRate != 0.75 {
		t.Errorf("pass rate = %v, want 0.75 (3/4 expectations)", m.PassRate)
	}
	if m.Measured == nil || m.Measured.InputTokens == nil || *m.Measured.InputTokens != 100 {
		t.Errorf("measured = %+v, want input 100", m.Measured)
	}
	if e := evalCaseMetricsOf(stError, plan.ItemMetrics{}); !e.Errored {
		t.Error("an error status should set Errored")
	}
}

func TestCaseDeltaBasisFallback(t *testing.T) {
	ev := plan.UnitRef{Skill: "s", Key: "fake/m1", Kind: plan.KindEvals}
	d := dashboardModel{prior: plan.PriorMetrics{}, liveBaseline: map[caseKey]results.EvalCaseMetrics{}}
	c := &caseState{kind: plan.KindEvals, label: "e1", status: stPass,
		metrics: plan.ItemMetrics{AssertPassed: new(1), AssertTotal: new(1)}}

	// No prior of any kind: no basis, no delta.
	if _, basis := d.caseDelta(ev, c); basis != basisNone {
		t.Errorf("basis = %v, want none", basis)
	}
	// A live baseline (this run) provides the baseline basis when no previous exists.
	d.liveBaseline[caseKey{ev, "e1"}] = results.EvalCaseMetrics{PassRate: new(0.0)}
	delta, basis := d.caseDelta(ev, c)
	if basis != basisBaseline {
		t.Errorf("basis = %v, want baseline", basis)
	}
	if delta.Rate == nil || *delta.Rate != 1.0 {
		t.Errorf("rate delta = %v, want +1.0 (0%% -> 100%%)", delta.Rate)
	}
}
