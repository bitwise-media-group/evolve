// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestQuitDialog covers the quit-confirmation flow.
func TestQuitDialog(t *testing.T) {
	m := testModel(t)
	m = step(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = step(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if m.screen != screenDashboard {
		t.Fatal("did not reach the dashboard")
	}

	// q opens the dialog without quitting.
	m, cmd := stepCmd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd != nil {
		t.Fatal("q should not quit immediately")
	}
	if !m.dash.confirmQuit || !strings.Contains(m.View(), "Are you sure") {
		t.Errorf("q should open the quit dialog:\n%s", m.View())
	}
	// n dismisses it.
	m, _ = stepCmd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.dash.confirmQuit {
		t.Error("n should dismiss the quit dialog")
	}
	// q then y quits.
	m, _ = stepCmd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if _, cmd = stepCmd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}); cmd == nil {
		t.Error("y in the dialog should quit")
	}
	// Two ctrl+c in a row quit immediately.
	m, _ = stepCmd(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m.dash.confirmQuit {
		t.Error("first ctrl+c should open the dialog")
	}
	if _, cmd = stepCmd(m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Error("second ctrl+c should quit")
	}
}
