// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bitwise-media-group/evolve/internal/run"
)

// TestExecutingPaneScrolls verifies the result region scrolls so a verdict below
// a long output stays reachable, and that a selection change resets the scroll.
func TestExecutingPaneScrolls(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	key := m1.Key()
	ev := run.UnitRef{Skill: "solo-skill", Key: key, Kind: run.KindEvals}
	filter := &run.Filter{
		Skills: map[string]bool{"solo-skill": true},
		Evals:  map[string]map[string]bool{"solo-skill": {"e1": true}},
	}
	d := newDashboard(cat, []run.UnitRef{ev}, filter)
	d.w, d.h = 100, 24 // a short pane so the output overflows the result region

	d.apply(unitStartedMsg{ref: ev, total: 1, mode: run.ModeRun})
	d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Index: 0, Label: "e1"}})
	d.apply(itemDoneMsg{ref: ev, item: run.ItemResult{
		Index: 0, Label: "e1", Status: run.StatusPass,
		Output:  strings.TrimRight(strings.Repeat("output line\n", 30), "\n"),
		Detail:  "  [PASS] e1: VERDICT-MARKER\n",
		Metrics: run.ItemMetrics{AvgRunSeconds: new(1.0)},
	}})

	// Focus the Details pane so its scroll keys are live.
	d.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	w, _, _, detailsH := d.rightDims()
	render := func() string { return d.renderDetails(w, detailsH-2) }
	if strings.Contains(render(), "VERDICT-MARKER") {
		t.Skip("pane tall enough to show the verdict without scrolling")
	}
	for i := 0; i < 30 && !strings.Contains(render(), "VERDICT-MARKER"); i++ {
		d.handleKey(tea.KeyMsg{Type: tea.KeyCtrlD})
	}
	if !strings.Contains(render(), "VERDICT-MARKER") {
		t.Errorf("verdict not reachable by scrolling:\n%s", render())
	}
	if d.detailScroll == 0 {
		t.Error("detailScroll should have advanced while scrolling")
	}
	// g returns the result to the top.
	d.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if d.detailScroll != 0 {
		t.Errorf("detailScroll = %d after g, want 0", d.detailScroll)
	}
}
