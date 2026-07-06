// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/results"
)

// The top-right "Rollup" pane: per-(skill, model) rows ranked by pass-rate change
// (Improvements / Regressions) or listed in full (Skills). Each row's headline is
// the eval pass rate and its delta vs the prior run; trigger rate (% triggered) is
// a secondary column. The execution pane shares aggRow/addUnit/aggUnits for its
// live group metrics.

type aggRow struct {
	passed, total            int
	durSum                   float64
	durN                     int
	in, out                  int
	cacheRead, cacheCreation int
	cost                     float64
	hasCost                  bool
}

// aggUnits rolls up a specific set of units identified by index — the skill and
// model rows in the Execution pane fold their live metrics with this.
func (d dashboardModel) aggUnits(idxs []int) aggRow {
	var r aggRow
	for _, i := range idxs {
		r.addUnit(d.units[i])
	}
	return r
}

// addUnit folds one unit's per-case metrics into the rollup.
func (r *aggRow) addUnit(u *unitState) {
	r.total += u.total
	for _, c := range u.cases {
		if c.status == stPass {
			r.passed++
		}
		m := c.metrics
		if m.AvgRunSeconds != nil {
			r.durSum += *m.AvgRunSeconds
			r.durN++
		}
		if m.InputTokens != nil {
			r.in += *m.InputTokens
		}
		if m.CacheReadTokens != nil {
			r.cacheRead += *m.CacheReadTokens
		}
		if m.CacheCreationTokens != nil {
			r.cacheCreation += *m.CacheCreationTokens
		}
		if m.OutputTokens != nil {
			r.out += *m.OutputTokens
		}
		if m.CostUSD != nil {
			r.cost += *m.CostUSD
			r.hasCost = true
		}
	}
}

// ── per-(skill, model) rollup rows ──────────────────────────────────────────

// skillRow is one (skill, model) pair's headline: eval pass rate and its delta,
// the trigger pass rate, and rolled-up cost.
type skillRow struct {
	skill        string
	key          string
	display      string
	evalPassed   int
	evalTotal    int
	evalComplete bool
	evalDelta    results.Delta
	evalBasis    deltaBasis
	trigCorrect  int // runs that behaved as should_trigger expects
	trigRuns     int
	cost         float64
	hasCost      bool
}

func (r skillRow) title() string { return r.skill + " · " + shortKey(r.key) }

// skillRows builds one row per (skill, model) from the static tree.
func (d dashboardModel) skillRows() []skillRow {
	var rows []skillRow
	for pi := range d.tree {
		for si := range d.tree[pi].skills {
			sg := d.tree[pi].skills[si]
			for _, mg := range sg.models {
				rows = append(rows, d.buildSkillRow(sg, mg))
			}
		}
	}
	return rows
}

func (d dashboardModel) buildSkillRow(sg skillGroup, mg modelGroup) skillRow {
	row := skillRow{skill: sg.title, key: mg.key, display: mg.display}
	for _, ui := range mg.units {
		u := d.units[ui]
		switch u.ref.Kind {
		case plan.KindEvals:
			d.fillEvalRow(&row, u)
		case plan.KindTriggers:
			d.fillTrigRow(&row, u)
		}
	}
	return row
}

// fillEvalRow folds the eval unit's live pass tally, cost, and pass-rate delta.
func (d dashboardModel) fillEvalRow(row *skillRow, u *unitState) {
	idx := []int{d.index[u.ref]}
	_, done := d.groupState(idx)
	row.evalComplete = done
	g := d.aggregateGroup(idx)
	row.evalPassed = g.livePassed
	row.evalTotal = g.liveTotal
	if g.liveHasCost {
		row.cost += g.liveCost
		row.hasCost = true
	}
	if done {
		row.evalDelta, row.evalBasis = g.delta()
	}
}

// fillTrigRow folds the trigger unit's correctness rate (runs that behaved as
// should_trigger expects, summed across queries) and cost. This is the same notion
// the execution pane shows per query, aggregated — so a correct negative counts its
// non-firing runs rather than dragging the rate down, and genuine flakiness still
// shows below 100%.
func (d dashboardModel) fillTrigRow(row *skillRow, u *unitState) {
	for _, c := range u.cases {
		if !c.status.terminal() {
			continue
		}
		if c.metrics.Hits != nil && c.metrics.Runs != nil {
			correct := *c.metrics.Hits
			if !c.shouldTrigger {
				correct = *c.metrics.Runs - *c.metrics.Hits
			}
			row.trigCorrect += correct
			row.trigRuns += *c.metrics.Runs
		}
		if c.metrics.CostUSD != nil {
			row.cost += *c.metrics.CostUSD
			row.hasCost = true
		}
	}
}

// rollupRows returns the rows for the active tab: improvements (gain desc),
// regressions (loss desc), or every row (skills, sorted by skill then model).
func (d dashboardModel) rollupRows() []skillRow {
	rows := d.skillRows()
	switch d.tab {
	case tabImprovements:
		return filterByDelta(rows, true)
	case tabRegressions:
		return filterByDelta(rows, false)
	default:
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].skill != rows[j].skill {
				return rows[i].skill < rows[j].skill
			}
			return rows[i].key < rows[j].key
		})
		return rows
	}
}

// filterByDelta keeps complete rows whose pass-rate delta has the requested sign,
// sorted by magnitude — most improved (or most regressed) first.
func filterByDelta(rows []skillRow, improvements bool) []skillRow {
	var out []skillRow
	for _, r := range rows {
		if !r.evalComplete || r.evalDelta.Rate == nil {
			continue
		}
		rate := *r.evalDelta.Rate
		if (improvements && rate > 0) || (!improvements && rate < 0) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if improvements {
			return *out[i].evalDelta.Rate > *out[j].evalDelta.Rate
		}
		return *out[i].evalDelta.Rate < *out[j].evalDelta.Rate
	})
	return out
}

// ── rendering ───────────────────────────────────────────────────────────────

// rollupTabNames are the Rollup pane's tab labels in tab order, shared by
// tabStrip (which renders them into the panel's top border) and tabAt (which
// hit-tests clicks against the same strings) so the two cannot drift.
var rollupTabNames = []string{"Improvements", "Regressions", "Skills"}

// tabStrip renders the rollup tabs for the panel's top border; only the active
// tab is recoloured.
func (d dashboardModel) tabStrip() string {
	names := rollupTabNames
	parts := make([]string, len(names))
	for i, n := range names {
		if tab(i) == d.tab {
			parts[i] = tabActiveStyle.Render(n)
		} else {
			parts[i] = mutedStyle.Render(n)
		}
	}
	return strings.Join(parts, " ")
}

// rollupColsFmt is the right-aligned metric block: pass rate, Δ rate, trigger
// rate, cost.
const rollupColsFmt = "%5s  %8s  %5s  %9s"

// rollupRightWidth is the rendered width of the metric block (the column widths
// plus the two-space gaps), used for the title-column width math.
const rollupRightWidth = 5 + 2 + 8 + 2 + 5 + 2 + 9

// aggRightGap keeps the rightmost column off the panel margin so the table's
// right padding matches the left pane's.
const aggRightGap = 1

func rollupHeader(w int) string {
	right := fmt.Sprintf(rollupColsFmt, "Pass", "Δ rate", "Trig", "Cost")
	tw := max(w-aggRightGap-ansi.StringWidth(right)-1, 6)
	label := "Skill · model"
	title := label + strings.Repeat(" ", max(tw-ansi.StringWidth(label), 0))
	return headerRollupStyle.Render(clip(title+" "+right, w))
}

func (d dashboardModel) renderTabs(w, h int) string {
	var b strings.Builder
	b.WriteString(rollupHeader(w))
	rows := d.rollupRows()
	if len(rows) == 0 {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(clip("  "+d.emptyTabHint(), w)))
		return b.String()
	}
	// The header pins to the top row; the rest of the pane scrolls the rows, with
	// ▲/▼ indicators when the list overflows (see scrollWindow).
	rowsH := max(h-1, 1)
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = d.rollupLine(r, w)
	}
	scroll := clampInt(d.rollupScroll, 0, max(0, len(lines)-rowsH))
	b.WriteString("\n")
	b.WriteString(scrollWindow(lines, scroll, rowsH))
	return b.String()
}

func (d dashboardModel) emptyTabHint() string {
	switch d.tab {
	case tabImprovements:
		return "no improvements yet"
	case tabRegressions:
		return "no regressions yet"
	default:
		return "no skills"
	}
}

func (d dashboardModel) rollupLine(r skillRow, w int) string {
	pass := emptyMetric
	if r.evalTotal > 0 {
		pass = fmt.Sprintf("%d%%", r.evalPassed*100/r.evalTotal)
	}
	// The Δ rate is shown only when the eval unit is complete and comparable, so it
	// does not bounce around as cases finish underneath.
	deltaStr, dir := emptyMetric, 0
	if r.evalComplete && r.evalDelta.Rate != nil {
		deltaStr = signedPct(r.evalDelta.Rate)
		if r.evalBasis == basisBaseline {
			deltaStr += baselineMark
		}
		dir = upGood(r.evalDelta.Rate)
	}
	trig := emptyMetric
	if r.trigRuns > 0 {
		trig = fmt.Sprintf("%d%%", r.trigCorrect*100/r.trigRuns)
	}
	cost := emptyMetric
	if r.hasCost {
		cost = fmtCost(r.cost)
	}
	right := fmt.Sprintf("%5s", pass) + "  " + colorCell(fmt.Sprintf("%8s", deltaStr), dir) +
		"  " + fmt.Sprintf("%5s", trig) + "  " + fmt.Sprintf("%9s", cost)
	tw := max(w-aggRightGap-rollupRightWidth-1, 6)
	title := truncate(r.title(), tw)
	title += strings.Repeat(" ", max(tw-ansi.StringWidth(title), 0))
	return clip(title+" "+right, w)
}
