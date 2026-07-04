// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── top-level composition ──────────────────────────────────────────────────

func (d dashboardModel) view() string {
	if d.confirmQuit {
		return d.quitDialog()
	}
	// Chrome rows above/below the panes: the title bar, a blank separator beneath
	// it, and the footer (leftDims/rightDims own the resulting body height). The
	// progress bar rides the title bar rather than taking a row of its own, so the
	// Execution and Rollup panes stay top-aligned.
	leftW := max(d.w/2, 28)
	rightW := max(d.w-leftW, 24)
	cW := panelContentWidth(rightW)

	// The left pane reflects the shared selection while unfocused, and becomes a
	// user-navigable tree (its own cursor + expansion) while focused.
	var nodes []nodeRef
	var hl int
	if d.execBrowse {
		nodes = d.buildNodeRefsWith(d.browseExpanded)
		hl = clampInt(d.execSel, 0, max(len(nodes)-1, 0))
	} else {
		nodes = d.buildNodeRefs()
		hl = d.followHighlight(nodes)
	}

	execH, legendH := d.leftDims()
	left := panel(1, "Execution", d.leftCount(nodes, hl), "",
		d.renderLeft(nodes, hl, panelContentWidth(leftW), execH-2),
		d.focus == paneExecution, leftW, execH, paneBaseColor(paneExecution))
	if legendH > 0 {
		body, _ := d.legend(leftW)
		legend := panel(0, "Legend", "", "", body, false, leftW, legendH, paneBaseColor(paneExecution))
		left = lipgloss.JoinVertical(lipgloss.Left, left, legend)
	}

	_, rollupH, runsH, detailsH := d.rightDims()
	rollup := panel(2, "Rollup", "", d.tabStrip(),
		d.renderTabs(cW, rollupH-2), d.focus == paneRollup, rightW, rollupH, paneBaseColor(paneRollup))
	runs := panel(3, "Runs", d.runsCount(), "",
		d.renderRuns(cW, runsH-2), d.focus == paneRuns, rightW, runsH, paneBaseColor(paneRuns))
	details := panel(4, "Details", "", "",
		d.renderDetails(cW, detailsH-2), d.focus == paneDetails, rightW, detailsH, paneBaseColor(paneDetails))
	right := lipgloss.JoinVertical(lipgloss.Left, rollup, runs, details)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	footer := footerHint.Render(clip(d.footerHints(), d.w))
	// "" is the blank separator row between the title bar and the panes.
	return lipgloss.JoinVertical(lipgloss.Left, d.titleBar(leftW, rightW), "", body, footer)
}

// legend builds the Execution pane's glyph legend for a pane of outer width w,
// returning the content and the panel height it needs. Entries are packed
// greedily into as few rows as fit the pane width — never split mid-entry, so
// nothing clips — which is the form legend's shape generalized past two rows
// (this pane is half the screen where the form's is two thirds).
func (d dashboardModel) legend(w int) (body string, h int) {
	items := []string{
		legendItem(passStyle.Render("✓"), "pass"),
		legendItem(threshPassStyle.Render("✓"), "≥ threshold"),
		legendItem(failStyle.Render("✗"), "fail"),
		legendItem(errStyle.Render("⚠"), "error"),
		legendItem(mutedStyle.Render("⊘"), "skipped"),
		legendItem(mutedStyle.Render("≈"), "counts only"),
		legendItem(mutedStyle.Render("·"), "no data"),
		legendItem("◌", "queued"),
		legendItem(mutedStyle.Render(baselineMark), "vs baseline"),
	}
	limit := panelContentWidth(w)
	var rows []string
	row := ""
	for _, it := range items {
		joined := it
		if row != "" {
			joined = row + "  " + it
		}
		if row != "" && lipgloss.Width(joined) > limit {
			rows = append(rows, row)
			row = it
			continue
		}
		row = joined
	}
	rows = append(rows, row)
	return strings.Join(rows, "\n"), len(rows) + 2
}

// leftDims splits the left column into the Execution panel's outer height and
// the legend's beneath it (0 when hidden). The legend is dropped on a short
// terminal — the form's paneH >= 13 rule, plus a floor of eight tree rows in
// case a narrow pane wraps the legend tall — so the tree keeps usable height;
// the two heights always sum to the body height, keeping the left column
// aligned with the right one. execPageStep shares this math so paging cannot
// drift from the rendered pane.
func (d dashboardModel) leftDims() (execH, legendH int) {
	bodyH := max(d.h-3, 4)
	if bodyH < 13 {
		return bodyH, 0
	}
	_, legendH = d.legend(max(d.w/2, 28))
	if bodyH-legendH < 8 {
		return bodyH, 0
	}
	return bodyH - legendH, legendH
}

// rightDims splits the right column into the Rollup, Runs, and Details panes,
// returning the shared content width and each pane's outer height. Runs is a
// compact list pane; Rollup takes a share of the rest; Details gets the bulk. The
// body height matches the left pane's (d.h minus the title bar, its blank
// separator, and the footer) so the columns stay aligned.
func (d dashboardModel) rightDims() (w, rollupH, runsH, detailsH int) {
	bodyH := max(d.h-3, 4)
	leftW := max(d.w/2, 28)
	rightW := max(d.w-leftW, 24)
	w = panelContentWidth(rightW)
	// Runs is a compact list: show up to 7 rows, but once the log overflows the
	// window, keep the row count odd so the selection centers with the top and
	// bottom rows free for the ▲/▼ indicators (see renderRuns/centerScroll).
	runsRows := clampInt(min(len(d.execLog), 7), 1, max(bodyH-8, 1))
	if runsRows < len(d.execLog) && runsRows%2 == 0 {
		runsRows--
	}
	runsH = runsRows + 2
	rest := bodyH - runsH
	rollupH = clampInt(rest*2/5, 5, max(rest-3, 5))
	detailsH = bodyH - runsH - rollupH
	if detailsH < 3 {
		rollupH = max(rollupH-(3-detailsH), 3)
		detailsH = bodyH - runsH - rollupH
	}
	return w, rollupH, runsH, detailsH
}

func paneBaseColor(p pane) color.Color {
	switch p {
	case paneExecution:
		return accentExec
	case paneRollup:
		return accentRollup
	case paneRuns:
		return accentRuns
	default:
		return accentDetails
	}
}

// footerHints shows the active pane's keys first, then the global shortcuts.
func (d dashboardModel) footerHints() string {
	var keys string
	switch d.focus {
	case paneExecution:
		keys = "[↑↓]/[jk] move · [→] expand · [←]/[h] collapse · [enter] open run · [g]/[G] top/bottom"
	case paneRollup:
		keys = "[←→]/[hl] switch tabs · [↑↓]/[jk] scroll · [g]/[G] top/bottom · [^d]/[^u] page down/up"
	case paneRuns:
		keys = "[↑↓]/[jk] scroll · [enter] open run · [g]/[G] top/bottom · [^d]/[^u] page down/up"
	default:
		keys = "[↑↓]/[jk] scroll · [g]/[G] jump to top/bottom · [^d]/[^u] page down/up"
	}
	return keys + " · [f] follow · [o] open dir · [l] open log · [q] quit"
}

// quitDialog is the centered confirmation shown before quitting.
func (d dashboardModel) quitDialog() string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentDetails).
		Padding(1, 4).
		Align(lipgloss.Center).
		Render(titleStyle.Render("Are you sure you want to quit?") + "\n\n" +
			mutedStyle.Render("[y]/[Enter]  quit      [n]/[Esc]  cancel"))
	return lipgloss.Place(max(d.w, 1), max(d.h, 1), lipgloss.Center, lipgloss.Center, box)
}

// titleBar is the top row: the run stats on the left and the run-wide progress
// bar on the right, sitting directly above the Rollup pane. Both ends are inset
// one column from the screen edges, and view() renders a blank row beneath it for
// separation. The bar rides this row instead of taking one of its own, so the
// Execution and Rollup panes start on the same line.
func (d dashboardModel) titleBar(leftW, rightW int) string {
	left := " " + clip(d.headerStats(), max(leftW-1, 1))
	pad := max(leftW-ansi.StringWidth(left), 0)
	// The bar spans the Rollup pane's columns, inset one column on the right.
	bar := d.overallProgressLine(max(rightW-1, 1))
	return clip(left+strings.Repeat(" ", pad)+bar, d.w)
}

// headerStats is the left side of the title bar: the run's pass/fail/running
// tallies, state, elapsed clock, and rolled-up cost.
func (d dashboardModel) headerStats() string {
	var pass, fail, errc, running, done, total int
	var cost float64
	for _, u := range d.units {
		for _, c := range u.cases {
			total++
			switch c.status {
			case stPass:
				pass, done = pass+1, done+1
			case stFail:
				fail, done = fail+1, done+1
			case stError:
				errc, done = errc+1, done+1
			case stRunning:
				running++
			case stSkipped, stCount:
				done++
			}
			if c.metrics.CostUSD != nil {
				cost += *c.metrics.CostUSD
			}
		}
	}
	state := "running"
	switch {
	case d.done:
		state = "done"
	case !d.started:
		state = "ready"
	}
	parts := []string{
		fmt.Sprintf("%d/%d", done, total),
		passStyle.Render(fmt.Sprintf("%d✓", pass)),
		failStyle.Render(fmt.Sprintf("%d✗", fail)),
		errStyle.Render(fmt.Sprintf("%d⚠", errc)),
		mutedStyle.Render(fmt.Sprintf("%d running", running)),
		mutedStyle.Render("(" + state + ")"),
	}
	head := evolveTitle() + "  " + strings.Join(parts, "  ")
	if d.started {
		head += "  " + mutedStyle.Render(fmtClock(d.now().Sub(d.startWall)))
	}
	if cost > 0 {
		head += "  " + mutedStyle.Render("~"+fmtCost(cost))
	}
	return head
}
