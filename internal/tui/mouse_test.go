// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strings"
	"testing"
)

// TestWindowIndexAtMatchesScrollWindowFunc pins windowIndexAt to the renderer:
// for every (n, focus, h) shape, the index it maps a row to is exactly the
// index scrollWindowFunc rendered on that row, and the ▲/▼ indicator rows map
// to -1 — so click targeting can never drift from what was drawn.
func TestWindowIndexAtMatchesScrollWindowFunc(t *testing.T) {
	shapes := []struct{ n, focus, h int }{
		{n: 3, focus: 0, h: 5},   // fits: no scrolling
		{n: 5, focus: 4, h: 5},   // exactly fits
		{n: 20, focus: 0, h: 5},  // top edge
		{n: 20, focus: 10, h: 5}, // centered
		{n: 20, focus: 19, h: 5}, // bottom edge
		{n: 20, focus: 18, h: 4}, // even window height
		{n: 2, focus: 1, h: 1},   // degenerate single-row window
	}
	for _, s := range shapes {
		t.Run(fmt.Sprintf("n=%d_focus=%d_h=%d", s.n, s.focus, s.h), func(t *testing.T) {
			rendered := scrollWindowFunc(s.n, s.focus, s.h, func(i int) string {
				return fmt.Sprintf("row#%d", i)
			})
			for row, line := range strings.Split(rendered, "\n") {
				got := windowIndexAt(s.n, s.focus, s.h, row)
				if strings.Contains(line, "▲") || strings.Contains(line, "▼") {
					if got != -1 {
						t.Errorf("row %d is an indicator, windowIndexAt = %d, want -1", row, got)
					}
					continue
				}
				if want := fmt.Sprintf("row#%d", got); !strings.Contains(line, want) {
					t.Errorf("row %d rendered %q, windowIndexAt = %d", row, line, got)
				}
			}
		})
	}
}

// TestWindowIndexAtOutOfRange covers the rows no content was drawn on.
func TestWindowIndexAtOutOfRange(t *testing.T) {
	cases := []struct{ n, focus, h, row, want int }{
		{n: 3, focus: 0, h: 5, row: 3, want: -1},  // past the short content
		{n: 3, focus: 0, h: 5, row: -1, want: -1}, // above the window
		{n: 3, focus: 0, h: 5, row: 5, want: -1},  // below the window
		{n: 0, focus: 0, h: 5, row: 0, want: -1},  // empty list
		{n: 3, focus: 0, h: 0, row: 0, want: -1},  // h clamps to 1; that row is the ▼ indicator
	}
	for _, c := range cases {
		if got := windowIndexAt(c.n, c.focus, c.h, c.row); got != c.want {
			t.Errorf("windowIndexAt(%d,%d,%d,%d) = %d, want %d", c.n, c.focus, c.h, c.row, got, c.want)
		}
	}
}

// TestTopWindowStartMatchesRenderRows pins topWindowStart to renderRows: the
// row rendered at each visible position is items[start+position].
func TestTopWindowStartMatchesRenderRows(t *testing.T) {
	items := make([]listItem, 10)
	for i := range items {
		items[i] = listItem{label: fmt.Sprintf("item#%d", i)}
	}
	for _, cursor := range []int{0, 3, 4, 9} {
		rows := 4
		start := topWindowStart(cursor, rows)
		rendered := renderRows(items, cursor, false, 40, rows, func(it listItem) string { return it.label })
		for pos, line := range strings.Split(rendered, "\n") {
			if want := items[start+pos].label; !strings.Contains(line, want) {
				t.Errorf("cursor=%d row %d rendered %q, want %q", cursor, pos, line, want)
			}
		}
	}
}

func TestRectContains(t *testing.T) {
	r := rect{x0: 2, y0: 1, x1: 10, y1: 5}
	if r.w() != 8 || r.h() != 4 {
		t.Errorf("w,h = %d,%d, want 8,4", r.w(), r.h())
	}
	for _, c := range []struct {
		x, y int
		want bool
	}{
		{2, 1, true}, {9, 4, true}, // inclusive corners
		{10, 4, false}, {9, 5, false}, // exclusive edges
		{1, 1, false}, {2, 0, false},
	} {
		if got := r.contains(c.x, c.y); got != c.want {
			t.Errorf("contains(%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestContentRect(t *testing.T) {
	got := contentRect(rect{x0: 10, y0: 2, x1: 40, y1: 12})
	want := rect{x0: 12, y0: 3, x1: 38, y1: 11}
	if got != want {
		t.Errorf("contentRect = %+v, want %+v", got, want)
	}
	// Content width matches panelContentWidth for the same outer width.
	if got.w() != panelContentWidth(30) {
		t.Errorf("content width = %d, want panelContentWidth(30) = %d", got.w(), panelContentWidth(30))
	}
}
