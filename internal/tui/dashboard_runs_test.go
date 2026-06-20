// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bitwise-media-group/evolve/internal/run"
)

// TestRunsFollowPause covers follow semantics: Runs tracks the newest execution,
// the Details pane pauses that follow while active, and F re-follows.
func TestRunsFollowPause(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	key := m1.Key()
	ev := run.UnitRef{Skill: "solo-skill", Key: key, Kind: run.KindEvals}
	filter := &run.Filter{
		Skills: map[string]bool{"solo-skill": true},
		Evals:  map[string]map[string]bool{"solo-skill": {"e1": true, "e2": true}},
	}
	d := newDashboard(cat, []run.UnitRef{ev}, filter)
	d.w, d.h = 100, 30

	d.apply(unitStartedMsg{ref: ev, total: 2, mode: run.ModeRun})
	d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Index: 0, Label: "e1"}})
	if d.currentRun() != 0 {
		t.Fatalf("Runs should follow the first execution, sel=%d", d.currentRun())
	}

	// Focusing Details pauses Runs' follow: a new execution does not move it.
	d.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Index: 1, Label: "e2"}})
	if d.currentRun() != 0 {
		t.Errorf("Details active should pause follow, sel=%d want 0", d.currentRun())
	}
	// Leaving Details resumes follow → snaps to the newest (index 1).
	d.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if d.currentRun() != 1 {
		t.Errorf("leaving Details should resume follow, sel=%d want 1", d.currentRun())
	}

	// k pauses follow; F re-follows the newest.
	d.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if d.runFollow {
		t.Error("k off the last row should pause follow")
	}
	d.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("F")})
	if !d.runFollow || d.currentRun() != 1 {
		t.Errorf("F should follow the newest, follow=%v sel=%d", d.runFollow, d.currentRun())
	}
}

// TestRunsPaneCentersSelection verifies the Runs window renders an odd row count
// and keeps a mid-list selection on the center row, with ▲/▼ overflow indicators
// on the outer rows.
func TestRunsPaneCentersSelection(t *testing.T) {
	cat := soloCatalog(t)
	_, m1 := soloModels()
	ev := run.UnitRef{Skill: "solo-skill", Key: m1.Key(), Kind: run.KindEvals}
	filter := &run.Filter{
		Skills: map[string]bool{"solo-skill": true},
		Evals:  map[string]map[string]bool{"solo-skill": {}},
	}
	d := newDashboard(cat, []run.UnitRef{ev}, filter)
	d.w, d.h = 120, 40 // tall enough that the Runs window hits its 7-row cap

	const n = 15
	d.apply(unitStartedMsg{ref: ev, total: n, mode: run.ModeRun})
	for i := range n {
		d.apply(itemStartedMsg{ref: ev, item: run.ItemStart{Index: i, Label: fmt.Sprintf("e%d", i)}})
	}
	if len(d.execLog) != n {
		t.Fatalf("execLog = %d, want %d", len(d.execLog), n)
	}

	// Focus Runs, go to the oldest row, then step to a comfortably mid-list one.
	d.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	d.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	for range 7 {
		d.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	if sel := d.currentRun(); sel != 7 {
		t.Fatalf("selection = %d, want 7 (mid-list)", sel)
	}

	w, _, runsH, _ := d.rightDims()
	h := runsH - 2
	if h%2 == 0 {
		t.Fatalf("Runs content height = %d, want odd", h)
	}

	rows := strings.Split(d.renderRuns(w, h), "\n")
	if len(rows) != h {
		t.Fatalf("rendered %d rows, want %d", len(rows), h)
	}

	selRow := -1
	for i, r := range rows {
		if strings.Contains(r, "›") { // the selected gutter glyph
			selRow = i
		}
	}
	if selRow != h/2 {
		t.Errorf("selected row at index %d, want center %d:\n%s", selRow, h/2, strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[0], "▲") {
		t.Errorf("top row should be the ▲ above indicator, got %q", rows[0])
	}
	if !strings.Contains(rows[h-1], "▼") {
		t.Errorf("bottom row should be the ▼ below indicator, got %q", rows[h-1])
	}
}
