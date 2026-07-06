// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

// Shared mouse hit-testing primitives. Both screens expose a layout() that
// reports where each pane was rendered as rects in screen cells; the mouse
// handlers hit-test against those and the window helpers here map a clicked
// row back to the underlying list index. The helpers mirror the rendering
// math (panel, renderRows/renderTree, scrollWindowFunc) — hit-testing must
// always read the same layout the view renders from, never re-derive it.

// wheelScrollStep is how many rows one wheel notch scrolls in an
// offset-scrolled pane (Details, Rollup). Selection-centered panes step by a
// single row instead — their window recenters on the selection, so a larger
// step would skip rows.
const wheelScrollStep = 3

// rect is a half-open screen rectangle [x0,x1) × [y0,y1) in zero-based
// terminal cells, (0,0) top-left — the same coordinates mouse events carry.
type rect struct{ x0, y0, x1, y1 int }

func (r rect) contains(x, y int) bool {
	return x >= r.x0 && x < r.x1 && y >= r.y0 && y < r.y1
}

func (r rect) w() int { return r.x1 - r.x0 }
func (r rect) h() int { return r.y1 - r.y0 }

// contentRect shrinks a panel's outer rectangle to its body area: one border
// row top and bottom, and the border plus one-column margin (two cells) on
// each side — the inverse of panel/panelContentWidth.
func contentRect(r rect) rect {
	return rect{r.x0 + 2, r.y0 + 1, r.x1 - 2, r.y1 - 1}
}

// windowIndexAt maps a body row of a selection-centered window — what
// scrollWindowFunc renders over n rows focused on focus in an h-row window —
// back to the underlying index. Rows holding the ▲/▼ overflow indicators and
// rows past the content return -1.
func windowIndexAt(n, focus, h, row int) int {
	if h < 1 {
		h = 1
	}
	if row < 0 || row >= h || row >= n {
		return -1
	}
	if n <= h {
		return row
	}
	scroll := centerScroll(n, focus, h)
	idx := scroll + row
	if (row == 0 && scroll > 0) || (row == h-1 && idx < n-1) {
		return -1
	}
	return idx
}

// topWindowStart is the first visible index of a cursor-anchored window over a
// flat list (renderRows/renderTree): the window stays at the top until the
// cursor walks past the last visible row, then slides just far enough to keep
// the cursor on the bottom row.
func topWindowStart(cursor, rows int) int {
	if rows < 1 {
		rows = 1
	}
	if cursor >= rows {
		return cursor - rows + 1
	}
	return 0
}
