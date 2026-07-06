// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import "testing"

// TestListSetCursor pins the click-target setter: in-range rows land exactly,
// out-of-range rows clamp, and an empty list stays put.
func TestListSetCursor(t *testing.T) {
	l := list{items: []listItem{{label: "a"}, {label: "b"}, {label: "c"}}}
	for _, tc := range []struct{ set, want int }{{2, 2}, {5, 2}, {-1, 0}} {
		l.setCursor(tc.set)
		if l.cursor != tc.want {
			t.Errorf("setCursor(%d) left cursor %d, want %d", tc.set, l.cursor, tc.want)
		}
	}
	var empty list
	empty.setCursor(3)
	if empty.cursor != 0 {
		t.Errorf("empty list cursor = %d, want 0", empty.cursor)
	}
}
