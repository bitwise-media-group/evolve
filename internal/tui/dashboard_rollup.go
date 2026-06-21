// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/bitwise-media-group/evolve/internal/run"
)

// The top-right "Rollup" pane: the tabbed aggregate table (Summary / Providers
// / Plugins / Skills) and the aggregation it renders.

type aggRow struct {
	title                    string
	passed, total            int
	durSum                   float64
	durN                     int
	in, out                  int
	cacheRead, cacheCreation int
	cost                     float64
	hasCost                  bool
}

func (d dashboardModel) agg(title string, include func(*unitState) bool) aggRow {
	r := aggRow{title: title}
	for _, u := range d.units {
		if include(u) {
			r.addUnit(u)
		}
	}
	return r
}

// aggUnits rolls up a specific set of units identified by index — the skill and
// model rows in the Execution pane share this folding with the Rollup tabs.
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

func (d dashboardModel) aggGroup(key func(*unitState) string) []aggRow {
	var order []string
	seen := map[string]bool{}
	for _, u := range d.units {
		if k := key(u); !seen[k] {
			seen[k] = true
			order = append(order, k)
		}
	}
	rows := make([]aggRow, 0, len(order))
	for _, k := range order {
		kk := k
		rows = append(rows, d.agg(k, func(u *unitState) bool { return key(u) == kk }))
	}
	return rows
}

func (d dashboardModel) tabRows() []aggRow {
	switch d.tab {
	case tabProviders:
		return d.aggGroup(func(u *unitState) string { return providerOf(u.ref.Key) })
	case tabPlugins:
		return d.aggGroup(func(u *unitState) string { return u.plugin })
	case tabSkills:
		return d.aggGroup(func(u *unitState) string { return u.ref.Skill })
	default:
		return []aggRow{
			d.agg("Overall", func(*unitState) bool { return true }),
			d.agg("triggers", func(u *unitState) bool { return u.ref.Kind == run.KindTriggers }),
			d.agg("evals", func(u *unitState) bool { return u.ref.Kind == run.KindEvals }),
		}
	}
}

func (d dashboardModel) renderTabs(w, h int) string {
	var b strings.Builder
	b.WriteString(aggHeader(w))
	rows := d.tabRows()
	for i, r := range rows {
		if i >= h-1 {
			break
		}
		b.WriteString("\n")
		b.WriteString(aggLine(r, w))
	}
	return b.String()
}

// tabStrip renders the rollup tabs for the panel's top border. The active tab is
// only recoloured (never widened) so switching tabs does not shift their titles.
func (d dashboardModel) tabStrip() string {
	names := []string{"Summary", "Providers", "Plugins", "Skills"}
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

// aggColsWidth is the right-aligned metric block of the rollup table.
const aggColsFmt = "%9s  %6s  %-18s  %9s"

// aggRightGap keeps the rightmost column off the panel margin so the table's
// right padding matches the left pane's.
const aggRightGap = 1

func aggHeader(w int) string {
	right := fmt.Sprintf(aggColsFmt, "Pass/Tot", "Avg", "In/Out/Total", "Cost")
	tw := max(w-aggRightGap-ansi.StringWidth(right)-1, 6)
	title := "Title" + strings.Repeat(" ", max(tw-5, 0))
	return headerStyle.Render(clip(title+" "+right, w))
}

func aggLine(r aggRow, w int) string {
	pt := fmt.Sprintf("%d/%d", r.passed, r.total)
	avg := emptyMetric
	if r.durN > 0 {
		avg = fmtDur(r.durSum / float64(r.durN))
	}
	// In is fresh input; Total folds in cache reads/writes so the headline
	// still reflects everything consumed — the In↔Total gap is the cache.
	tok := fmtTok(r.in) + "/" + fmtTok(r.out) + "/" + fmtTok(r.in+r.cacheRead+r.cacheCreation+r.out)
	cost := emptyMetric
	if r.hasCost {
		cost = fmtCost(r.cost)
	}
	right := fmt.Sprintf(aggColsFmt, pt, avg, tok, cost)
	tw := max(w-aggRightGap-ansi.StringWidth(right)-1, 6)
	title := truncate(r.title, tw)
	title += strings.Repeat(" ", max(tw-ansi.StringWidth(title), 0))
	return clip(title+" "+right, w)
}
