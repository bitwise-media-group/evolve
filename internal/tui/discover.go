// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// DiscoverItem is one vendor-advertised model in the discover picker. Source
// is "" for a model the registry does not know yet; otherwise it names where
// the model is already registered ("builtin" or the config file name) so the
// row renders as informational rather than selectable.
type DiscoverItem struct {
	ProviderID   string
	ProviderName string
	ID           string // bare vendor id, e.g. "claude-sonnet-5"
	Name         string // vendor display name ("" when the API has none)
	Source       string // "" = new; else "builtin" or the config file name
}

// qualified is the provider-scoped id the row displays and the fuzzy query
// matches against ("anthropic/claude-sonnet-5").
func (d DiscoverItem) qualified() string { return d.ProviderID + "/" + d.ID }

// RunDiscover shows the full-screen fuzzy multi-select picker over the
// discovered models and returns the items chosen for injection, in catalog
// order. dest names the config file selections land in (shown in the footer).
// ok is false when the user cancelled.
func RunDiscover(items []DiscoverItem, dest string) (chosen []DiscoverItem, ok bool, err error) {
	final, err := tea.NewProgram(newDiscover(items, dest)).Run()
	if err != nil {
		return nil, false, err
	}
	m, isModel := final.(discoverModel)
	if !isModel || !m.confirmed {
		return nil, false, nil
	}
	for i, it := range m.items {
		if m.selected[i] {
			chosen = append(chosen, it)
		}
	}
	return chosen, true, nil
}

// discoverModel is the picker: a live filter query over the discovered rows,
// multi-selection by item index (stable across refilters), and a confirm/cancel
// exit pair. It is its own bubbletea program, separate from the run form.
type discoverModel struct {
	items     []DiscoverItem
	dest      string // config file selections are written to
	query     string
	matches   []int // indices into items, ranked when query is non-empty
	cursor    int   // position within matches
	selected  map[int]bool
	confirmed bool
	w, h      int
}

func newDiscover(items []DiscoverItem, dest string) discoverModel {
	m := discoverModel{items: items, dest: dest, selected: map[int]bool{}}
	m.refilter()
	return m
}

func (m discoverModel) Init() tea.Cmd { return nil }

func (m discoverModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		next, done := m.update(msg.String())
		if done {
			return next, tea.Quit
		}
		return next, nil
	}
	return m, nil
}

// update handles one key and reports whether the program should exit (either
// confirmed or cancelled — the confirmed field distinguishes them). Typing
// filters; navigation and toggling follow the fzf conventions (tab toggles so
// the query can contain spaces) with ctrl+j/k standing in for the vim keys the
// live filter consumes.
func (m discoverModel) update(key string) (discoverModel, bool) {
	switch key {
	case "up", "ctrl+k":
		m.move(-1)
	case "down", "ctrl+j":
		m.move(1)
	case "pgup":
		m.move(-10)
	case "pgdown":
		m.move(10)
	case "home":
		m.cursor = 0
	case "end":
		m.cursor = max(len(m.matches)-1, 0)
	case "tab":
		m.toggle()
		m.move(1)
	case "shift+tab":
		m.toggle()
		m.move(-1)
	case "ctrl+a":
		m.toggleAll()
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.refilter()
		}
	case "enter":
		// Enter with no explicit selection adds the row under the cursor —
		// the single-model flow needs no tab at all.
		if len(m.selected) == 0 {
			m.toggle()
		}
		if len(m.selected) == 0 {
			return m, false // nothing selectable chosen; stay open
		}
		m.confirmed = true
		return m, true
	case "esc":
		if m.query != "" {
			m.query = ""
			m.refilter()
			return m, false
		}
		return m, true
	case "ctrl+c":
		return m, true
	case "space":
		m.input(" ")
	default:
		if r := []rune(key); len(r) == 1 && unicode.IsPrint(r[0]) {
			m.input(key)
		}
	}
	return m, false
}

func (m *discoverModel) input(s string) {
	m.query += s
	m.refilter()
}

func (m *discoverModel) move(delta int) {
	if len(m.matches) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.matches)-1)
}

// toggle flips the selection under the cursor; rows that are already
// registered (Source != "") are informational and stay unselectable.
func (m *discoverModel) toggle() {
	if m.cursor < 0 || m.cursor >= len(m.matches) {
		return
	}
	i := m.matches[m.cursor]
	if m.items[i].Source != "" {
		return
	}
	if m.selected[i] {
		delete(m.selected, i)
	} else {
		m.selected[i] = true
	}
}

// toggleAll selects every selectable visible row, or clears them all when
// every one is already selected.
func (m *discoverModel) toggleAll() {
	all := true
	for _, i := range m.matches {
		if m.items[i].Source == "" && !m.selected[i] {
			all = false
			break
		}
	}
	for _, i := range m.matches {
		if m.items[i].Source != "" {
			continue
		}
		if all {
			delete(m.selected, i)
		} else {
			m.selected[i] = true
		}
	}
}

// refilter recomputes the visible rows: catalog order when the query is empty,
// score-ranked (stable) when filtering. The cursor is clamped, not preserved —
// after a query change the top match is the natural target.
func (m *discoverModel) refilter() {
	m.matches = m.matches[:0]
	type ranked struct{ idx, score int }
	var rs []ranked
	for i, it := range m.items {
		hay := it.qualified() + " " + it.Name
		score, ok := fuzzyScore(m.query, hay)
		if !ok {
			continue
		}
		rs = append(rs, ranked{i, score})
	}
	if m.query != "" {
		sort.SliceStable(rs, func(a, b int) bool { return rs[a].score > rs[b].score })
	}
	for _, r := range rs {
		m.matches = append(m.matches, r.idx)
	}
	m.cursor = min(m.cursor, max(len(m.matches)-1, 0))
}

// fuzzyScore reports whether every space-separated term of query matches s as
// a case-insensitive subsequence, with a score that prefers consecutive runs
// and matches at word boundaries (start, or after one of -/._ and space).
// An empty query matches everything with score 0.
func fuzzyScore(query, s string) (int, bool) {
	low := strings.ToLower(s)
	total := 0
	for term := range strings.SplitSeq(strings.ToLower(query), " ") {
		if term == "" {
			continue
		}
		score, ok := matchTerm(term, low)
		if !ok {
			return 0, false
		}
		total += score
	}
	return total, true
}

func matchTerm(term, low string) (int, bool) {
	score, prev := 0, -2
	runes := []rune(low)
	pos := 0
	for _, q := range term {
		found := false
		for i := pos; i < len(runes); i++ {
			if runes[i] != q {
				continue
			}
			switch {
			case i == prev+1:
				score += 3 // consecutive run
			case i == 0 || isBoundary(runes[i-1]):
				score += 2 // word boundary
			}
			prev, pos, found = i, i+1, true
			break
		}
		if !found {
			return 0, false
		}
	}
	// Earlier first-hits read as better matches; a small penalty breaks ties.
	return score - pos/8, true
}

func isBoundary(r rune) bool {
	return r == '-' || r == '/' || r == '.' || r == '_' || r == ' '
}

func (m discoverModel) View() tea.View {
	const headerH, footerH = 2, 1
	paneH := max(m.h-headerH-footerH, 6)
	w := max(m.w, 24)

	body := m.renderBody(panelContentWidth(w), paneH-2)
	count := fmt.Sprintf("%d selected · %s shown", len(m.selected), countLabel(len(m.matches), len(m.items)))
	pane := panel(0, "Discover Models", count, "", body, true, w, paneH, accentDetails)

	keyHelp := "[type] filter · [↑↓]/[ctrl+jk] move · [tab] toggle · [ctrl+a] toggle all"
	hint := footerHint.Render(clip(
		keyHelp+" · [enter] add to "+m.dest+" · [esc] clear/cancel", max(m.w, 1)))

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, " "+evolveTitle(), "", pane, hint))
	v.AltScreen = true
	return v
}

// renderBody draws the query line and the visible rows, scrolled to keep the
// cursor on screen.
func (m discoverModel) renderBody(w, h int) string {
	var b strings.Builder
	b.WriteString(selectedStyle.Render("› ") + m.query + mutedStyle.Render("█"))
	b.WriteByte('\n')

	rows := max(h-1, 1)
	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	end := min(start+rows, len(m.matches))
	for pos := start; pos < end; pos++ {
		i := m.matches[pos]
		row := clip(m.renderRow(i), w-2)
		if pos == m.cursor {
			row = selectedStyle.Render("› ") + row
		} else {
			row = "  " + row
		}
		b.WriteString(row)
		if pos < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// renderRow draws one model row: checkbox (or a muted dot for rows that are
// already registered), the qualified id, the vendor display name, and the
// where-it-lives badge.
func (m discoverModel) renderRow(i int) string {
	it := m.items[i]
	var b strings.Builder
	if it.Source != "" {
		b.WriteString(mutedStyle.Render("[·] " + it.qualified()))
	} else {
		b.WriteString(checkGlyph(m.selected[i]) + " " + it.qualified())
	}
	if it.Name != "" {
		b.WriteString(mutedStyle.Render("  " + it.Name))
	}
	if it.Source != "" {
		b.WriteString(mutedStyle.Render(" · already in " + it.Source))
	}
	return b.String()
}
