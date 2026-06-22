// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/bitwise-media-group/evolve/internal/run"
)

// Cross-pane rendering primitives shared by the dashboard view files: status
// glyphs, scroll/window math, and the small number/time formatters.

// ── status ──────────────────────────────────────────────────────────────────

// caseGlyph renders a case row's status glyph, tinting the live spinner yellow
// while its without-skill baseline runs (ahead of the run under test) and blue
// otherwise — see glyph for the settled-status glyphs.
func (d dashboardModel) caseGlyph(c *caseState) string {
	if c.baselineRunning {
		return baselineStyle.Render(strings.TrimSpace(d.spin.View()))
	}
	return d.glyph(c.status)
}

func (d dashboardModel) glyph(s status) string {
	switch s {
	case stRunning:
		return runStyle.Render(strings.TrimSpace(d.spin.View()))
	case stPass:
		return passStyle.Render("✓")
	case stFail:
		return failStyle.Render("✗")
	case stError:
		return errStyle.Render("⚠")
	case stSkipped:
		return mutedStyle.Render("⊘")
	case stCount:
		return mutedStyle.Render("≈")
	case stNoData:
		return mutedStyle.Render("·")
	default:
		return pendStyle.Render("◌")
	}
}

func statusWord(s status) string {
	switch s {
	case stRunning:
		return "running"
	case stPass:
		return "pass"
	case stFail:
		return "fail"
	case stError:
		return "error"
	case stSkipped:
		return "skipped"
	case stCount:
		return "counts only"
	case stNoData:
		return "no data"
	default:
		return "pending"
	}
}

func (d dashboardModel) inflightElapsed(ref run.UnitRef, label string) (float64, bool) {
	for _, ifl := range d.inflight {
		if ifl.ref == ref && ifl.label == label {
			return d.now().Sub(ifl.start).Seconds(), true
		}
	}
	return 0, false
}

func shortKey(key string) string { return key }

// ── scrolling + windowing ─────────────────────────────────────────────────────

// centerScroll returns the scroll offset that keeps focus vertically centered in
// an h-row window over n lines, clamped so the window stays in range. With an odd
// h the focused line sits dead center, leaving the top and bottom rows free for
// the ▲/▼ indicators; near the list ends the window settles flush against the
// top or bottom and focus drifts off-center rather than scrolling past the edge.
func centerScroll(n, focus, h int) int {
	if n <= h {
		return 0
	}
	return clampInt(focus-h/2, 0, n-h)
}

// scrollWindow renders h rows of lines from scroll, replacing the first/last
// visible row with a ▲/▼ indicator when content is hidden above/below.
func scrollWindow(lines []string, scroll, h int) string {
	if len(lines) <= h {
		return strings.Join(lines, "\n")
	}
	scroll = clampInt(scroll, 0, len(lines)-h)
	out := make([]string, 0, h)
	for i := range h {
		idx := scroll + i
		line := lines[idx]
		switch {
		case i == 0 && scroll > 0:
			line = mutedStyle.Render(fmt.Sprintf("  ┄ ▲ %d above ┄", scroll))
		case i == h-1 && idx < len(lines)-1:
			line = mutedStyle.Render(fmt.Sprintf("  ┄ ▼ %d below (ctrl+d) ┄", len(lines)-1-idx))
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// ── small formatters ────────────────────────────────────────────────────────

// emptyMetric is the placeholder for an absent metric cell. It is the figure
// dash (U+2012), which Unicode sizes to a digit's width — unlike the em dash, it
// right-aligns flush with the numbers it stands in for inside a fixed-width
// numeric column.
const emptyMetric = "‒"

func fmtTok(n int) string {
	switch {
	case n <= 0:
		return "0"
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	}
}

func fmtTokPtr(p *int) string {
	if p == nil {
		return emptyMetric
	}
	return fmtTok(*p)
}

func intOr0(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func fmtCost(f float64) string {
	if f < 1 {
		return fmt.Sprintf("$%.4f", f)
	}
	return fmt.Sprintf("$%.2f", f)
}

func fmtCostPtr(p *float64) string {
	if p == nil {
		return emptyMetric
	}
	return fmtCost(*p)
}

func fmtDur(s float64) string {
	if s < 100 {
		return fmt.Sprintf("%.1fs", s)
	}
	return fmt.Sprintf("%.0fs", s)
}

func fmtDurPtr(p *float64) string {
	if p == nil {
		return emptyMetric
	}
	return fmtDur(*p)
}

func fmtClock(d time.Duration) string {
	s := max(int(d.Seconds()), 0)
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
