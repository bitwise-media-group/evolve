// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"testing"

	"github.com/bitwise-media-group/evolve/internal/plan"
)

// TestPendingGlyph proves the "queued to run" indicator is tinted by the prior
// result it will re-run against: green after a pass, orange after a threshold
// pass, red after a fail or error, and the plain foreground when there is no
// prior result.
func TestPendingGlyph(t *testing.T) {
	cases := []struct {
		name  string
		prior status
		want  string
	}{
		{"prior pass tints green", stPass, passStyle.Render("◌")},
		{"prior threshold pass tints orange", stPassThreshold, threshPassStyle.Render("◌")},
		{"prior fail tints red", stFail, failStyle.Render("◌")},
		{"prior error tints red", stError, failStyle.Render("◌")},
		{"no prior is plain", stPending, "◌"},
		{"no-data is plain", stNoData, "◌"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pendingGlyph(tc.prior); got != tc.want {
				t.Errorf("pendingGlyph(%v) = %q, want %q", tc.prior, got, tc.want)
			}
		})
	}
}

// TestPassThresholdGlyph pins the threshold-pass presentation: an orange check
// for the settled glyph and a status word that reads apart from a clean pass.
func TestPassThresholdGlyph(t *testing.T) {
	d := newDashboard(plan.Plan{}, soloCatalog(t), plan.PriorMetrics{}, testThresholds)
	if got, want := d.glyph(stPassThreshold), threshPassStyle.Render("✓"); got != want {
		t.Errorf("glyph(stPassThreshold) = %q, want the orange check %q", got, want)
	}
	if got, want := statusWord(stPassThreshold), "pass (met threshold)"; got != want {
		t.Errorf("statusWord(stPassThreshold) = %q, want %q", got, want)
	}
}

// TestCaseGlyphQueuedVsPrior pins the indicator rule: a case queued to run this
// session shows the pending dot (tinted by its prior result) so it reads apart from
// a read-only prior row's settled check/cross, a running case spins, and a freshly
// settled or no-data row keeps its plain glyph.
func TestCaseGlyphQueuedVsPrior(t *testing.T) {
	d := newDashboard(plan.Plan{}, soloCatalog(t), plan.PriorMetrics{}, testThresholds)
	cases := []struct {
		name string
		c    *caseState
		want string
	}{
		{"queued with prior pass → green pending", &caseState{status: stPass}, passStyle.Render("◌")},
		{"queued with prior fail → red pending", &caseState{status: stFail}, failStyle.Render("◌")},
		{"queued with no prior → plain pending", &caseState{status: stPending}, "◌"},
		{"prior pass (not queued) → settled check", &caseState{status: stPass, prior: true}, passStyle.Render("✓")},
		{"prior fail (not queued) → settled cross", &caseState{status: stFail, prior: true}, failStyle.Render("✗")},
		{"freshly settled this run → check", &caseState{status: stPass, liveDone: true}, passStyle.Render("✓")},
		{"running → spinner", &caseState{status: stRunning}, d.glyph(stRunning)},
		{"no-data → muted dot", &caseState{status: stNoData, prior: true}, mutedStyle.Render("·")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := d.caseGlyph(tc.c); got != tc.want {
				t.Errorf("caseGlyph(%+v) = %q, want %q", tc.c, got, tc.want)
			}
		})
	}
}

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
