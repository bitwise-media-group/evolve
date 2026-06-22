// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"

	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/results"
)

// Live deltas: the dashboard compares each finished case against the run it is
// replacing. The basis is previous when a prior committed result exists, else the
// without-skill baseline (evals only — seeded from disk or streamed live this run
// via BaselineDone). The baseline basis carries an indicator so the user knows the
// delta is not against a prior run. Deltas are only ever shown once a node is
// complete, so they do not flicker as children finish underneath.

type deltaBasis int

const (
	basisNone deltaBasis = iota
	basisPrevious
	basisBaseline
)

// baselineMark is the muted indicator appended when a delta is measured against
// the baseline rather than a previous run.
const baselineMark = "ᵇ"

// caseKey identifies one case within a unit, for the live-baseline lookup.
type caseKey struct {
	ref   plan.UnitRef
	label string
}

// evalCaseMetricsOf projects a finished eval case's live status and metrics into
// the results shape the delta helpers compare. The rate is the expectation tally.
func evalCaseMetricsOf(st status, m plan.ItemMetrics) results.EvalCaseMetrics {
	out := results.EvalCaseMetrics{AvgRunSeconds: m.AvgRunSeconds, Errored: st == stError}
	if m.AssertPassed != nil && m.AssertTotal != nil && *m.AssertTotal > 0 {
		r := float64(*m.AssertPassed) / float64(*m.AssertTotal)
		out.PassRate = &r
	}
	if st == stPass || st == stFail {
		p := st == stPass
		out.Passed = &p
	}
	if m.InputTokens != nil || m.OutputTokens != nil || m.CostUSD != nil ||
		m.CacheReadTokens != nil || m.CacheCreationTokens != nil {
		out.Measured = &results.Measured{
			InputTokens:         m.InputTokens,
			CacheReadTokens:     m.CacheReadTokens,
			CacheCreationTokens: m.CacheCreationTokens,
			OutputTokens:        m.OutputTokens,
			CostUSD:             m.CostUSD,
		}
	}
	return out
}

// triggerCaseMetricsOf projects a finished trigger case into the results shape.
func triggerCaseMetricsOf(st status, m plan.ItemMetrics) results.TriggerCaseMetrics {
	out := results.TriggerCaseMetrics{Hits: m.Hits, Runs: m.Runs, AvgRunSeconds: m.AvgRunSeconds}
	if st == stPass || st == stFail {
		p := st == stPass
		out.Passed = &p
	}
	if m.InputTokens != nil || m.CostUSD != nil {
		out.Estimate = &results.Estimate{}
		if m.InputTokens != nil {
			out.Estimate.InputTokens = *m.InputTokens
		}
		if m.CostUSD != nil {
			out.Estimate.InputCostUSD = m.CostUSD
		}
	}
	return out
}

// evalCasePrior resolves an eval case's comparison basis: previous, else baseline
// (live this run, else seeded), else none.
func (d dashboardModel) evalCasePrior(ref plan.UnitRef, label string) (results.EvalCaseMetrics, deltaBasis) {
	if m, ok := d.prior.EvalPrevious(ref, label); ok {
		return m, basisPrevious
	}
	if m, ok := d.liveBaseline[caseKey{ref, label}]; ok {
		return m, basisBaseline
	}
	if m, ok := d.prior.EvalBaseline(ref, label); ok {
		return m, basisBaseline
	}
	return results.EvalCaseMetrics{}, basisNone
}

// caseDelta is the per-metric delta and basis for one finished case.
func (d dashboardModel) caseDelta(ref plan.UnitRef, c *caseState) (results.Delta, deltaBasis) {
	if c.kind == plan.KindEvals {
		prior, basis := d.evalCasePrior(ref, c.label)
		if basis == basisNone {
			return results.Delta{}, basisNone
		}
		return results.EvalCaseDelta(evalCaseMetricsOf(c.status, c.metrics), prior), basis
	}
	prior, ok := d.prior.TriggerPrevious(ref, c.label)
	if !ok {
		return results.Delta{}, basisNone
	}
	return results.TriggerCaseDelta(triggerCaseMetricsOf(c.status, c.metrics), prior, c.shouldTrigger), basisPrevious
}

// ── group aggregation (execution group rows) ────────────────────────────────

// groupAgg accumulates live and prior tallies across a group's terminal cases,
// blending trigger and eval cases the way the live group metric already does.
type groupAgg struct {
	livePassed, liveTotal int
	liveAvgSum            float64
	liveAvgN              int
	liveIn, liveOut       int
	liveCost              float64
	liveHasCost           bool

	priorPassed, priorTotal int
	priorAvgSum             float64
	priorAvgN               int
	priorIn, priorOut       int
	priorCost               float64
	priorHasCost            bool
	hasPrior                bool
	usedBaseline, usedPrev  bool
}

// priorScalars is one case's prior metrics flattened to the figures a group
// blends (token source differs by tier; the caller already knows which).
type priorScalars struct {
	passed  *bool
	avg     *float64
	in, out *int
	cost    *float64
	basis   deltaBasis
}

func (d dashboardModel) casePriorScalars(ref plan.UnitRef, c *caseState) (priorScalars, bool) {
	if c.kind == plan.KindEvals {
		m, basis := d.evalCasePrior(ref, c.label)
		if basis == basisNone {
			return priorScalars{}, false
		}
		ps := priorScalars{passed: m.Passed, avg: m.AvgRunSeconds, basis: basis}
		if m.Measured != nil {
			ps.in, ps.out, ps.cost = m.Measured.InputTokens, m.Measured.OutputTokens, m.Measured.CostUSD
		}
		return ps, true
	}
	m, ok := d.prior.TriggerPrevious(ref, c.label)
	if !ok {
		return priorScalars{}, false
	}
	ps := priorScalars{passed: m.Passed, avg: m.AvgRunSeconds, basis: basisPrevious}
	if m.Estimate != nil {
		in := m.Estimate.InputTokens
		ps.in, ps.cost = &in, m.Estimate.InputCostUSD
	}
	return ps, true
}

// aggregateGroup folds a group's terminal cases into live and prior tallies.
func (d dashboardModel) aggregateGroup(units []int) groupAgg {
	var g groupAgg
	for _, ui := range units {
		u := d.units[ui]
		for _, c := range u.cases {
			if !c.status.terminal() {
				continue
			}
			g.liveTotal++
			if c.status == stPass {
				g.livePassed++
			}
			if c.metrics.AvgRunSeconds != nil {
				g.liveAvgSum += *c.metrics.AvgRunSeconds
				g.liveAvgN++
			}
			g.liveIn += intOr0(c.metrics.InputTokens)
			g.liveOut += intOr0(c.metrics.OutputTokens)
			if c.metrics.CostUSD != nil {
				g.liveCost += *c.metrics.CostUSD
				g.liveHasCost = true
			}
			ps, ok := d.casePriorScalars(u.ref, c)
			if !ok {
				continue
			}
			g.hasPrior = true
			switch ps.basis {
			case basisBaseline:
				g.usedBaseline = true
			case basisPrevious:
				g.usedPrev = true
			}
			if ps.passed != nil {
				g.priorTotal++
				if *ps.passed {
					g.priorPassed++
				}
			}
			if ps.avg != nil {
				g.priorAvgSum += *ps.avg
				g.priorAvgN++
			}
			g.priorIn += intOr0(ps.in)
			g.priorOut += intOr0(ps.out)
			if ps.cost != nil {
				g.priorCost += *ps.cost
				g.priorHasCost = true
			}
		}
	}
	return g
}

// delta turns a finished group's tallies into a per-metric delta and its basis.
func (g groupAgg) delta() (results.Delta, deltaBasis) {
	if !g.hasPrior {
		return results.Delta{}, basisNone
	}
	var d results.Delta
	if g.priorTotal > 0 && g.liveTotal > 0 {
		live := float64(g.livePassed) / float64(g.liveTotal)
		prior := float64(g.priorPassed) / float64(g.priorTotal)
		r := results.Round6(live - prior)
		d.Rate = &r
	}
	if g.liveAvgN > 0 && g.priorAvgN > 0 {
		v := results.Round1(g.liveAvgSum/float64(g.liveAvgN) - g.priorAvgSum/float64(g.priorAvgN))
		d.AvgRunSeconds = &v
	}
	in := g.liveIn - g.priorIn
	d.InputTokens = &in
	out := g.liveOut - g.priorOut
	d.OutputTokens = &out
	if g.liveHasCost && g.priorHasCost {
		c := results.Round6(g.liveCost - g.priorCost)
		d.CostUSD = &c
	}
	basis := basisPrevious
	if g.usedBaseline && !g.usedPrev {
		basis = basisBaseline
	}
	return d, basis
}

// ── coloring ────────────────────────────────────────────────────────────────

// colorCell tints a (pre-padded) metric cell: green when the metric improved, red
// when it worsened, untouched when unchanged or not comparable.
func colorCell(s string, dir int) string {
	switch {
	case dir > 0:
		return passStyle.Render(s)
	case dir < 0:
		return failStyle.Render(s)
	default:
		return s
	}
}

// upGood scores a metric where an increase is an improvement (a rate).
func upGood(d *float64) int {
	switch {
	case d == nil || *d == 0:
		return 0
	case *d > 0:
		return 1
	default:
		return -1
	}
}

// downGood scores a metric where a decrease is an improvement (cost, time).
func downGood(d *float64) int {
	switch {
	case d == nil || *d == 0:
		return 0
	case *d < 0:
		return 1
	default:
		return -1
	}
}

// downGoodInt is downGood for an integer metric (tokens).
func downGoodInt(d *int) int {
	switch {
	case d == nil || *d == 0:
		return 0
	case *d < 0:
		return 1
	default:
		return -1
	}
}

// signedPct renders a rate delta as a signed percentage, blank when not comparable.
func signedPct(d *float64) string {
	if d == nil {
		return emptyMetric
	}
	return fmt.Sprintf("%+.0f%%", *d*100)
}
