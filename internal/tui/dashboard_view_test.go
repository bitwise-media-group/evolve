// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

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

// TestDashLayoutTilesBody pins the layout geometry: the two columns tile the
// body height exactly, the x split sits at leftW, and the footer row lands
// directly under the body — so hit-testing covers every cell without overlap.
func TestDashLayoutTilesBody(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	d := dashFromFilter(cat, []harness.Selection{m1}, nil, plan.PriorMetrics{})

	for _, size := range []struct{ w, h int }{{120, 40}, {100, 30}, {80, 14}} {
		d.w, d.h = size.w, size.h
		l := d.layout()
		bodyH := max(d.h-3, 4)

		if l.exec.y0 != 2 || l.rollup.y0 != 2 {
			t.Errorf("%dx%d: body must start at row 2, got exec %d rollup %d", size.w, size.h, l.exec.y0, l.rollup.y0)
		}
		if leftH := l.exec.h() + l.legend.h(); leftH != bodyH {
			t.Errorf("%dx%d: left column height %d, want bodyH %d", size.w, size.h, leftH, bodyH)
		}
		if rightH := l.rollup.h() + l.runs.h() + l.details.h(); rightH != bodyH {
			t.Errorf("%dx%d: right column height %d, want bodyH %d", size.w, size.h, rightH, bodyH)
		}
		if l.runs.y0 != l.rollup.y1 || l.details.y0 != l.runs.y1 {
			t.Errorf("%dx%d: right column rects must stack without gaps", size.w, size.h)
		}
		if l.exec.x1 != l.leftW || l.rollup.x0 != l.leftW || l.rollup.x1 != l.leftW+l.rightW {
			t.Errorf("%dx%d: x split not at leftW=%d: exec %+v rollup %+v", size.w, size.h, l.leftW, l.exec, l.rollup)
		}
		if l.footerY != 2+bodyH {
			t.Errorf("%dx%d: footerY = %d, want %d", size.w, size.h, l.footerY, 2+bodyH)
		}
	}
}

// TestDashLayoutMatchesView aligns the layout rects against the rendered
// frame: each pane's title sits on the row its rect starts at, and the open
// hints sit on the footer row — the drift guard between layout() and view().
func TestDashLayoutMatchesView(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	d := dashFromFilter(cat, []harness.Selection{m1}, nil, plan.PriorMetrics{})
	d.w, d.h = 120, 40

	l := d.layout()
	lines := strings.Split(d.view(), "\n")
	for _, tc := range []struct {
		row  int
		want string
	}{
		{row: l.exec.y0, want: "Execution"},
		{row: l.legend.y0, want: "Legend"},
		{row: l.rollup.y0, want: "Rollup"},
		{row: l.runs.y0, want: "Runs"},
		{row: l.details.y0, want: "Details"},
		{row: l.footerY, want: "open dir"},
	} {
		if tc.row >= len(lines) || !strings.Contains(lines[tc.row], tc.want) {
			t.Errorf("row %d must hold %q:\n%s", tc.row, tc.want, d.view())
		}
	}
}

// TestTabAt hit-tests the rollup tab strip against the rendered top border:
// the column each tab name renders at resolves to that tab, and rows or
// columns outside the strip miss.
func TestTabAt(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	d := dashFromFilter(cat, []harness.Selection{m1}, nil, plan.PriorMetrics{})
	d.w, d.h = 120, 40

	l := d.layout()
	border := ansi.Strip(strings.Split(d.view(), "\n")[l.rollup.y0])
	for i, name := range rollupTabNames {
		idx := strings.Index(border, name)
		if idx < 0 {
			t.Fatalf("tab %q not on the rollup border row %q", name, border)
		}
		x := ansi.StringWidth(border[:idx])
		for _, col := range []int{x, x + ansi.StringWidth(name) - 1} {
			if got, ok := d.tabAt(l, col, l.rollup.y0); !ok || got != tab(i) {
				t.Errorf("tabAt(%d, %d) = (%v, %v), want (%v, true)", col, l.rollup.y0, got, ok, tab(i))
			}
		}
	}
	if _, ok := d.tabAt(l, l.rollup.x0+1, l.rollup.y0); ok {
		t.Error("border fill left of the strip must not resolve to a tab")
	}
	if _, ok := d.tabAt(l, l.rollup.x0+10, l.rollup.y0+1); ok {
		t.Error("a body row must not resolve to a tab")
	}

	// A pane too narrow for the strip truncates it; clicks are not guessed at.
	d.w = 56
	l = d.layout()
	for x := l.rollup.x0; x < l.rollup.x1; x++ {
		if _, ok := d.tabAt(l, x, l.rollup.y0); ok {
			t.Fatalf("truncated strip must not hit-test (x=%d)", x)
		}
	}
}

// TestFooterOpenTarget maps footer clicks onto the open-dir/open-log hints,
// measured off the same footerHints string the view renders.
func TestFooterOpenTarget(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	d := dashFromFilter(cat, []harness.Selection{m1}, nil, plan.PriorMetrics{})
	d.w, d.h = 120, 40

	hints := d.footerHints()
	for seg, want := range map[string]byte{"[o] open dir": 'o', "[l] open log": 'l'} {
		x := ansi.StringWidth(hints[:strings.Index(hints, seg)])
		for _, col := range []int{x, x + len(seg) - 1} {
			if got := d.footerOpenTarget(col); got != want {
				t.Errorf("footerOpenTarget(%d) = %q, want %q", col, got, want)
			}
		}
		if got := d.footerOpenTarget(x - 1); got == want {
			t.Errorf("footerOpenTarget just left of %q must miss", seg)
		}
	}
	if got := d.footerOpenTarget(0); got != 0 {
		t.Errorf("footerOpenTarget(0) = %q, want no target", got)
	}
}
