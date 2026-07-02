// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"
)

func discoverItems() []DiscoverItem {
	return []DiscoverItem{
		{ProviderID: "anthropic", ProviderName: "Anthropic", ID: "claude-sonnet-5", Name: "Claude Sonnet 5"},
		{ProviderID: "anthropic", ProviderName: "Anthropic", ID: "claude-opus-4-8", Name: "Claude Opus 4.8", Source: "builtin"},
		{ProviderID: "openai", ProviderName: "OpenAI", ID: "gpt-5.5", Name: ""},
		{ProviderID: "google", ProviderName: "Google", ID: "gemini-3.5-flash", Name: "Gemini 3.5 Flash"},
	}
}

// keys feeds a sequence of key names through update, returning the final model
// and whether the last key exited the program.
func keys(t *testing.T, m discoverModel, presses ...string) (discoverModel, bool) {
	t.Helper()
	done := false
	for _, k := range presses {
		if done {
			t.Fatalf("key %q pressed after the program exited", k)
		}
		m, done = m.update(k)
	}
	return m, done
}

func TestFuzzyScore(t *testing.T) {
	tests := []struct {
		query, s string
		match    bool
	}{
		{"", "anything", true},
		{"cs5", "anthropic/claude-sonnet-5", true},
		{"sonnet", "anthropic/claude-sonnet-5", true},
		{"claude sonnet", "anthropic/claude-sonnet-5 Claude Sonnet 5", true}, // space = AND of terms
		{"SONNET", "anthropic/claude-sonnet-5", true},                        // case-insensitive
		{"gpt", "anthropic/claude-sonnet-5", false},
		{"sonnet gpt", "anthropic/claude-sonnet-5", false}, // one term missing
	}
	for _, tc := range tests {
		if _, ok := fuzzyScore(tc.query, tc.s); ok != tc.match {
			t.Errorf("fuzzyScore(%q, %q) match = %v, want %v", tc.query, tc.s, ok, tc.match)
		}
	}
}

func TestFuzzyScoreRanksExactRunsHigher(t *testing.T) {
	run, _ := fuzzyScore("sonnet", "anthropic/claude-sonnet-5")
	scattered, _ := fuzzyScore("sonnet", "s-o-n-n-e-t scattered")
	if run <= scattered {
		t.Errorf("consecutive run scored %d, scattered %d; want run higher", run, scattered)
	}
}

func TestDiscoverTypingFilters(t *testing.T) {
	m := newDiscover(discoverItems(), ".evolve.yaml")
	if len(m.matches) != 4 {
		t.Fatalf("initial matches = %d, want 4", len(m.matches))
	}
	m, _ = keys(t, m, "g", "p", "t")
	if len(m.matches) != 1 || m.items[m.matches[0]].ID != "gpt-5.5" {
		t.Fatalf("after typing gpt: matches = %v", m.matches)
	}
	// backspace re-widens; esc clears the query without exiting.
	m, _ = keys(t, m, "backspace")
	if m.query != "gp" {
		t.Errorf("after backspace query = %q, want gp", m.query)
	}
	m, done := keys(t, m, "esc")
	if done || m.query != "" || len(m.matches) != 4 {
		t.Errorf("esc should clear the query and stay open: query=%q matches=%d done=%v",
			m.query, len(m.matches), done)
	}
}

func TestDiscoverToggleAndConfirm(t *testing.T) {
	m := newDiscover(discoverItems(), ".evolve.yaml")
	// tab toggles the first row and advances; the second row is builtin and
	// refuses to toggle.
	m, _ = keys(t, m, "tab", "tab")
	if !m.selected[0] {
		t.Error("row 0 should be selected after tab")
	}
	if m.selected[1] {
		t.Error("builtin row 1 must not be selectable")
	}
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2 after two tabs", m.cursor)
	}
	m, done := keys(t, m, "enter")
	if !done || !m.confirmed {
		t.Fatalf("enter should confirm: done=%v confirmed=%v", done, m.confirmed)
	}
}

func TestDiscoverEnterAddsCursorRowWhenNothingSelected(t *testing.T) {
	m := newDiscover(discoverItems(), ".evolve.yaml")
	m, done := keys(t, m, "enter")
	if !done || !m.confirmed || !m.selected[0] {
		t.Fatalf("bare enter should select the cursor row and confirm: done=%v confirmed=%v selected=%v",
			done, m.confirmed, m.selected)
	}

	// On an unselectable row, bare enter stays open instead of injecting nothing.
	m2 := newDiscover(discoverItems(), ".evolve.yaml")
	m2, _ = keys(t, m2, "ctrl+j") // cursor to the builtin row
	m2, done = keys(t, m2, "enter")
	if done || m2.confirmed {
		t.Errorf("enter on a builtin row with no selection should stay open: done=%v confirmed=%v", done, m2.confirmed)
	}
}

func TestDiscoverToggleAll(t *testing.T) {
	m := newDiscover(discoverItems(), ".evolve.yaml")
	m, _ = keys(t, m, "ctrl+a")
	// 3 selectable rows (the builtin one is skipped).
	if len(m.selected) != 3 {
		t.Fatalf("ctrl+a selected %d rows, want 3", len(m.selected))
	}
	m, _ = keys(t, m, "ctrl+a")
	if len(m.selected) != 0 {
		t.Errorf("second ctrl+a should clear the selection, got %d", len(m.selected))
	}
}

func TestDiscoverSelectionSurvivesRefilter(t *testing.T) {
	m := newDiscover(discoverItems(), ".evolve.yaml")
	m, _ = keys(t, m, "tab")         // select claude-sonnet-5
	m, _ = keys(t, m, "g", "p", "t") // filter it out of view
	m, _ = keys(t, m, "esc")         // clear the query
	if !m.selected[0] {
		t.Error("selection should survive refiltering")
	}
}

func TestDiscoverEscCancelsAndCtrlC(t *testing.T) {
	m := newDiscover(discoverItems(), ".evolve.yaml")
	m, done := keys(t, m, "esc")
	if !done || m.confirmed {
		t.Errorf("esc with empty query should cancel: done=%v confirmed=%v", done, m.confirmed)
	}
	m2 := newDiscover(discoverItems(), ".evolve.yaml")
	m2, _ = keys(t, m2, "x")
	m2, done = keys(t, m2, "ctrl+c")
	if !done || m2.confirmed {
		t.Errorf("ctrl+c should always cancel: done=%v confirmed=%v", done, m2.confirmed)
	}
}

func TestDiscoverViewSmoke(t *testing.T) {
	m := newDiscover(discoverItems(), ".evolve.yaml")
	m.w, m.h = 100, 24
	body := m.View().Content
	for _, want := range []string{"Discover Models", "claude-sonnet-5", "already in builtin", "0 selected"} {
		if !strings.Contains(body, want) {
			t.Errorf("view missing %q", want)
		}
	}
}
