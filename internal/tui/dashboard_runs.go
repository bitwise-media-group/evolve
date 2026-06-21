// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/bitwise-media-group/evolve/internal/run"
)

// The right-column "Runs" pane: the execution log list (newest last), windowed
// and centered on the selection.

// runsCount shows the selection position within the execution log.
func (d dashboardModel) runsCount() string {
	if sel := d.currentRun(); sel >= 0 {
		return fmt.Sprintf("%d/%d", sel+1, len(d.execLog))
	}
	return ""
}

// renderRuns draws the Runs pane: the execution log (newest last) windowed to h
// rows centered on the selection, with ▲/▼ indicators when entries are hidden.
func (d dashboardModel) renderRuns(w, h int) string {
	sel := d.currentRun()
	if sel < 0 {
		if !d.started {
			return mutedStyle.Render("waiting to start…")
		}
		return mutedStyle.Render("no executions yet.")
	}
	lines := make([]string, len(d.execLog))
	for i, e := range d.execLog {
		lines[i] = d.execLine(e, w, i == sel)
	}
	return scrollWindow(lines, centerScroll(len(lines), sel, h), h)
}

// execLine renders one row of the execution log: status glyph, tier, label, and
// the live elapsed (in-flight) or final duration.
func (d dashboardModel) execLine(e execItem, w int, selected bool) string {
	c := d.caseFor(e)
	gutter := " "
	if selected {
		gutter = selectedStyle.Render("›")
	}
	kind := "trig"
	if e.ref.Kind == run.KindEvals {
		kind = "eval"
	}
	g := pendStyle.Render("◌")
	if c != nil {
		g = d.glyph(c.status)
	}
	dur := emptyMetric
	if el, ok := d.inflightElapsed(e.ref, e.label); ok {
		dur = fmtDur(el)
	} else if c != nil && c.metrics.AvgRunSeconds != nil {
		dur = fmtDur(*c.metrics.AvgRunSeconds)
	}
	prefix := gutter + " " + g + " " + mutedStyle.Render(kind) + "  "
	avail := max(w-ansi.StringWidth(prefix)-ansi.StringWidth(dur)-2, 6)
	label := truncate(e.label, avail)
	pad := max(avail-ansi.StringWidth(label), 0)
	body := label + strings.Repeat(" ", pad) + " " + mutedStyle.Render(dur)
	if !selected {
		body = mutedStyle.Render(body)
	}
	return clip(prefix+body, w)
}

// caseFor resolves the live case state an execItem points at.
func (d dashboardModel) caseFor(e execItem) *caseState {
	if u := d.unit(e.ref); u != nil {
		return u.byLabel[e.label]
	}
	return nil
}
