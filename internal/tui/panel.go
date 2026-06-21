// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	cornerTL = "╭"
	cornerTR = "╮"
	cornerBL = "╰"
	cornerBR = "╯"
	hBar     = "─"
	vBar     = "│"
)

// panel frames body in a rounded border with the title embedded in the top
// border ("─[N]─Title──"), an optional right-aligned segment in the top border
// (topRight, e.g. a tab strip), and an optional count in the bottom-right
// ("──count─"), lazygit-style. num<=0 omits the [N] tag; empty topRight/count
// omit those. w and h are the panel's total outer dimensions.
// accent, when supplied, paints the border and title in a fixed colour
// regardless of focus — used for the dashboard's non-selectable panels.
func panel(num int, title, count, topRight, body string, focused bool, w, h int, accent ...lipgloss.Color) string {
	if w < 4 {
		w = 4
	}
	if h < 3 {
		h = 3
	}
	innerW := w - 2
	innerH := h - 2

	bs, ts := borderBlurStyle, titleBlurStyle
	switch {
	case len(accent) > 0 && accent[0] != "":
		// Accent panels (the dashboard): the title keeps the pane's bright hue at
		// all times for readability; only the border dims when the pane is blurred.
		ts = lipgloss.NewStyle().Bold(true).Foreground(accent[0])
		border := accent[0]
		if !focused {
			border = dim(accent[0])
		}
		bs = lipgloss.NewStyle().Foreground(border)
	case focused:
		bs, ts = borderFocusStyle, titleFocusStyle
	}

	// Top border: ╭─[N]─Title────────tabs─╮
	head := title
	if num > 0 {
		head = "[" + strconv.Itoa(num) + "]" + hBar + title
	}
	if maxHead := innerW - 1; ansi.StringWidth(head) > maxHead {
		head = ansi.Truncate(head, max0(maxHead), "")
	}
	var top string
	if topRight == "" {
		fillTop := max0(innerW - 1 - ansi.StringWidth(head))
		top = bs.Render(cornerTL+hBar) + ts.Render(head) + bs.Render(strings.Repeat(hBar, fillTop)+cornerTR)
	} else {
		if maxTR := innerW - 2 - ansi.StringWidth(head); maxTR > 0 && ansi.StringWidth(topRight) > maxTR {
			topRight = ansi.Truncate(topRight, max0(maxTR), "")
		}
		fillTop := max0(innerW - 2 - ansi.StringWidth(head) - ansi.StringWidth(topRight))
		top = bs.Render(cornerTL+hBar) + ts.Render(head) +
			bs.Render(strings.Repeat(hBar, fillTop)) + topRight + bs.Render(hBar+cornerTR)
	}

	// Bottom border: ╰────────count─╯
	var bottom string
	if count == "" {
		bottom = bs.Render(cornerBL + strings.Repeat(hBar, innerW) + cornerBR)
	} else {
		if maxCnt := innerW - 1; ansi.StringWidth(count) > maxCnt {
			count = ansi.Truncate(count, max0(maxCnt), "")
		}
		fillBot := innerW - 1 - ansi.StringWidth(count)
		bottom = bs.Render(cornerBL+strings.Repeat(hBar, max0(fillBot))) + countStyle.Render(count) + bs.Render(hBar+cornerBR)
	}

	// Body rows, padded/truncated to the inner box with a one-column margin on
	// each side so content never touches the border. Callers size their body to
	// panelContentWidth(w); a stray over-long line is still truncated here.
	const pad = 1
	contentW := max0(innerW - 2*pad)
	margin := strings.Repeat(" ", pad)
	lines := strings.Split(body, "\n")
	var b strings.Builder
	b.WriteString(top)
	b.WriteString("\n")
	for i := range innerH {
		ln := ""
		if i < len(lines) {
			ln = lines[i]
		}
		if wln := ansi.StringWidth(ln); wln > contentW {
			ln = ansi.Truncate(ln, contentW, "")
		} else if wln < contentW {
			ln += strings.Repeat(" ", contentW-wln)
		}
		b.WriteString(bs.Render(vBar))
		b.WriteString(margin)
		b.WriteString(ln)
		b.WriteString(margin)
		b.WriteString(bs.Render(vBar))
		b.WriteString("\n")
	}
	b.WriteString(bottom)
	return b.String()
}

// panelContentWidth is the usable body width inside a panel of outer width w,
// accounting for the border and the one-column margin on each side.
func panelContentWidth(w int) int {
	return max0(w - 4)
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
