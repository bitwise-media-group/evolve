// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package tui renders the interactive selection form and live run dashboard for
// `evolve run`. It is a presentation layer over internal/run: the engine
// reports progress through run.Reporter, which tuiReporter forwards into this
// program as messages.
package tui

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/run"
)

type screen int

const (
	screenForm screen = iota
	screenDashboard
)

// Model is the root bubbletea model: it shows the selection form, then the live
// dashboard once the user chooses RUN.
type Model struct {
	screen screen
	form   formModel
	dash   dashboardModel
	cat    []run.SkillCatalog
	prior  run.PriorMetrics
	runReq chan<- RunRequest
	w, h   int
}

// New builds the model. cat is the full catalog; sels is every available
// provider/model pair; needs is the per-resolved-model, per-case run matrix
// (from run.Needs) that seeds the form's tri-state selection; notes are the
// per-case preselection reasons shown beside each case; evalFilter forces
// non-matching evals off. The chosen RunRequest is sent on runReq when the user
// runs; the channel is closed by the caller if they cancel.
func New(cat []run.SkillCatalog, sels []provider.Selection, needs map[string]map[run.CaseRef]bool,
	notes map[run.CaseRef]string, evalFilter string, prior run.PriorMetrics, runReq chan<- RunRequest) Model {
	return Model{
		screen: screenForm,
		form:   newForm(cat, sels, needs, notes, evalFilter),
		cat:    cat,
		prior:  prior,
		runReq: runReq,
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.form.w, m.form.h = msg.Width, msg.Height
		m.dash.w, m.dash.h = msg.Width, msg.Height
		return m, nil

	case spinner.TickMsg:
		if m.screen == screenDashboard {
			var cmd tea.Cmd
			m.dash.spin, cmd = m.dash.spin.Update(msg)
			if m.dash.done {
				return m, nil
			}
			return m, cmd
		}
		return m, nil

	case tea.KeyPressMsg:
		if m.screen == screenForm {
			var action formAction
			m.form, action = m.form.update(msg.String())
			switch action {
			case actionCancel:
				return m, tea.Quit
			case actionRun:
				return m.startRun()
			}
			return m, nil
		}
		if m.dash.handleKey(msg) {
			return m, tea.Quit
		}
		return m, nil

	case unitStartedMsg, unitSkippedMsg, itemStartedMsg, baselineStartedMsg, itemDoneMsg,
		baselineDoneMsg, unitFinishedMsg, warnMsg, runDoneMsg:
		m.dash.apply(msg)
		return m, nil
	}
	return m, nil
}

// startRun transitions to the dashboard and dispatches the run request to the
// engine goroutine.
func (m Model) startRun() (tea.Model, tea.Cmd) {
	req := m.form.request()
	tiers := run.Tiers{Triggers: true, Evals: true} // the form spans both tiers
	// Plan every on-disk unit (nil filter), not just the queued ones, so the
	// dashboard can show preserved cases from prior runs; the merged filter still
	// marks which cases are queued this session.
	units := make([]run.UnitRef, 0, len(req.Models)*2)
	for _, sel := range req.Models {
		units = append(units, run.PlanFor(m.cat, sel, nil, tiers)...)
	}
	m.dash = newDashboard(m.cat, units, mergeFilters(req.Filters), m.prior)
	m.dash.w, m.dash.h = m.w, m.h
	m.screen = screenDashboard
	return m, tea.Batch(
		func() tea.Msg { m.runReq <- req; return nil },
		m.dash.spin.Tick,
	)
}

// mergeFilters unions the per-model filters into one filter for the dashboard
// catalog, which shows every case that is part of the run for any model.
func mergeFilters(filters map[string]*run.Filter) *run.Filter {
	merged := &run.Filter{
		Skills:   map[string]bool{},
		Triggers: map[string]map[string]bool{},
		Evals:    map[string]map[string]bool{},
	}
	add := func(dst, src map[string]map[string]bool) {
		for skill, set := range src {
			if dst[skill] == nil {
				dst[skill] = map[string]bool{}
			}
			for k, v := range set {
				if v {
					dst[skill][k] = true
				}
			}
		}
	}
	for _, f := range filters {
		for s := range f.Skills {
			merged.Skills[s] = true
		}
		add(merged.Triggers, f.Triggers)
		add(merged.Evals, f.Evals)
	}
	return merged
}

func (m Model) View() tea.View {
	content := m.dash.view()
	if m.screen == screenForm {
		content = m.form.view()
	}
	// Alt-screen is declared on the View in bubbletea v2 (the WithAltScreen
	// program option is gone); the renderer enters full-window mode for us.
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}
