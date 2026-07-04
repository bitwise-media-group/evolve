// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/plan"
)

// TestDashboardLegendResponsive pins the greedy packing: one row when every
// entry fits the pane width, more as it narrows — and no row ever exceeds the
// pane's content width, so entries never clip mid-label (the fixed two-row
// split this replaced clipped at half-screen widths).
func TestDashboardLegendResponsive(t *testing.T) {
	d := newDashboard(plan.Plan{}, soloCatalog(t), plan.PriorMetrics{}, testThresholds)
	if body, h := d.legend(200); h != 3 || strings.Contains(body, "\n") {
		t.Errorf("wide legend = (h=%d, %q), want a single-row height 3", h, body)
	}
	for _, w := range []int{60, 40, 28} {
		body, h := d.legend(w)
		rows := strings.Split(body, "\n")
		if h != len(rows)+2 {
			t.Errorf("legend(%d) h = %d, want rows+2 = %d", w, h, len(rows)+2)
		}
		if len(rows) < 2 {
			t.Errorf("legend(%d) = one row, want wrapping at this width", w)
		}
		for _, row := range rows {
			if got := lipgloss.Width(row); got > panelContentWidth(w) {
				t.Errorf("legend(%d) row %q width %d overflows the pane content width %d", w, row, got, panelContentWidth(w))
			}
		}
	}
}

// TestDashboardLegendLayout pins the left column split: the legend panel sits
// under the Execution pane with the two heights summing to the body height (so
// the columns stay aligned), the paging step follows the reduced pane, and a
// short terminal drops the legend to keep the tree usable.
func TestDashboardLegendLayout(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	d := dashFromFilter(cat, []harness.Selection{m1}, nil, plan.PriorMetrics{})
	d.w, d.h = 120, 40

	out := d.view()
	for _, want := range []string{"Legend", "≥ threshold"} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard view missing %q:\n%s", want, out)
		}
	}
	if got := lipgloss.Height(out); got != d.h {
		t.Errorf("view height = %d, want %d (legend must not overflow the column)", got, d.h)
	}

	bodyH := max(d.h-3, 4)
	execH, legendH := d.leftDims()
	if legendH == 0 || execH+legendH != bodyH {
		t.Errorf("leftDims = (%d, %d), want a shown legend summing to bodyH %d", execH, legendH, bodyH)
	}
	if got, want := d.execPageStep(), max(execH-4, 1); got != want {
		t.Errorf("execPageStep = %d, want %d (the legend-reduced pane)", got, want)
	}

	// A narrow pane wraps the legend taller; the split still sums to bodyH.
	d.w = 60
	execH, legendH = d.leftDims()
	if legendH < 4 || execH+legendH != bodyH {
		t.Errorf("narrow leftDims = (%d, %d), want a wrapped legend summing to bodyH %d", execH, legendH, bodyH)
	}

	// A short terminal drops the legend rather than squeezing the tree.
	d.w, d.h = 120, 14 // bodyH 11 < 13
	if execH, legendH = d.leftDims(); legendH != 0 || execH != 11 {
		t.Errorf("short-terminal leftDims = (%d, %d), want the full body and no legend", execH, legendH)
	}
	if out := d.view(); strings.Contains(out, "Legend") {
		t.Error("short terminal must drop the legend panel")
	}
}
