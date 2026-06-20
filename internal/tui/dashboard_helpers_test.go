// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import "testing"

func TestCenterScroll(t *testing.T) {
	cases := []struct {
		name        string
		n, focus, h int
		want        int
	}{
		{"fits no scroll", 5, 3, 7, 0},
		{"mid centers", 15, 7, 7, 4},     // focus sits at window index 3 (center)
		{"top clamps to 0", 15, 1, 7, 0}, // can't center near the top
		{"bottom clamps", 15, 14, 7, 8},  // can't center near the bottom
		{"first item", 15, 0, 7, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := centerScroll(tc.n, tc.focus, tc.h); got != tc.want {
				t.Errorf("centerScroll(%d, %d, %d) = %d, want %d", tc.n, tc.focus, tc.h, got, tc.want)
			}
		})
	}
}
