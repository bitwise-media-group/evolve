// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── top-level composition ──────────────────────────────────────────────────

func (d dashboardModel) view() string {
	if d.confirmQuit {
		return d.quitDialog()
	}
	headerH, footerH := 1, 1
	bodyH := max(d.h-headerH-footerH, 4)
	leftW := max(d.w/2, 28)
	rightW := max(d.w-leftW, 24)
	cW := panelContentWidth(rightW)

	nodes := d.buildNodeRefs()
	live := d.liveFocus(nodes)

	// Left Execution pane: untagged (num 0) and never focusable, so always dim.
	left := panel(0, "Execution", d.leftCount(nodes), "",
		d.renderLeft(nodes, live, panelContentWidth(leftW), bodyH-2), false, leftW, bodyH, dim(colCyberPink))

	_, rollupH, runsH, detailsH := d.rightDims()
	rollup := panel(1, "Rollup", "", d.tabStrip(),
		d.renderTabs(cW, rollupH-2), false, rightW, rollupH, d.paneColor(paneRollup))
	runs := panel(2, "Runs", d.runsCount(), "",
		d.renderRuns(cW, runsH-2), false, rightW, runsH, d.paneColor(paneRuns))
	details := panel(3, "Details", "", "",
		d.renderDetails(cW, detailsH-2), false, rightW, detailsH, d.paneColor(paneDetails))
	right := lipgloss.JoinVertical(lipgloss.Left, rollup, runs, details)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	footer := footerHint.Render(clip(d.footerHints(), d.w))
	return lipgloss.JoinVertical(lipgloss.Left, d.headerLine(), body, footer)
}

// rightDims splits the right column into the Rollup, Runs, and Details panes,
// returning the shared content width and each pane's outer height. Runs is a
// compact list pane; Rollup takes a share of the rest; Details gets the bulk.
func (d dashboardModel) rightDims() (w, rollupH, runsH, detailsH int) {
	bodyH := max(d.h-2, 4)
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

// paneColor is a pane's accent: its bright hue when focused, a dim shade when not.
func (d dashboardModel) paneColor(p pane) lipgloss.Color {
	if d.focus == p {
		return paneBaseColor(p)
	}
	return dim(paneBaseColor(p))
}

func paneBaseColor(p pane) lipgloss.Color {
	switch p {
	case paneRollup:
		return colCyberGreen
	case paneRuns:
		return colCyberBlue
	default:
		return colCyberOrange
	}
}

// footerHints shows the active pane's keys first, then the global shortcuts.
func (d dashboardModel) footerHints() string {
	var keys string
	switch d.focus {
	case paneRollup:
		keys = "[←→]/[hl] switch tabs"
	case paneRuns:
		keys = "[↑↓]/[jk] scroll · [g]/[G] jump to top/bottom · [^d]/[^u] page down/up"
	default:
		keys = "[↑↓]/[jk] scroll · [g]/[G] jump to top/bottom · [^d]/[^u] page down/up"
	}
	return keys + " · [f] follow · [o] open dir · [l] open log · [q] quit"
}

// quitDialog is the centered confirmation shown before quitting.
func (d dashboardModel) quitDialog() string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colCyberOrange).
		Padding(1, 4).
		Align(lipgloss.Center).
		Render(titleStyle.Render("Are you sure you want to quit?") + "\n\n" +
			mutedStyle.Render("[y]/[Enter]  quit      [n]/[Esc]  cancel"))
	return lipgloss.Place(max(d.w, 1), max(d.h, 1), lipgloss.Center, lipgloss.Center, box)
}

func (d dashboardModel) headerLine() string {
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
	head := titleStyle.Render("evolve") + "  " + strings.Join(parts, "  ")
	if d.started {
		head += "  " + mutedStyle.Render(fmtClock(d.now().Sub(d.startWall)))
	}
	if cost > 0 {
		head += "  " + mutedStyle.Render("~"+fmtCost(cost))
	}
	return clip(head, d.w)
}
