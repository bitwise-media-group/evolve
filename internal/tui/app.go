// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package tui renders the interactive selection form and live run dashboard for
// `evolve run`, plus the fuzzy multi-select picker for `evolve models discover`
// (see discover.go). It is a presentation layer over internal/run: the engine
// reports progress through run.Reporter, which tuiReporter forwards into this
// program as messages.
package tui

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/bitwise-media-group/evolve/internal/plan"
)

type screen int

const (
	screenForm screen = iota
	screenDashboard
)

// Model is the root bubbletea model: it shows the selection form, then the live
// dashboard once the user chooses RUN.
type Model struct {
	screen     screen
	form       formModel
	dash       dashboardModel
	cat        []plan.SkillCatalog
	prior      plan.PriorMetrics
	thresholds Thresholds
	runReq     chan<- RunRequest
	w, h       int
}

// New builds the model. session owns the form's filter and selection state (the
// harnesses/models it lists and the new/modified/failed baseline); cat is the
// full catalog the form's case tree is built from; evalFilter forces non-matching
// evals off; prior seeds the dashboard's deltas; thresholds are the report gates
// the dashboard classifies rollups against. The chosen RunRequest is sent on
// runReq when the user runs; the channel is closed by the caller if they cancel.
func New(session *plan.Session, cat []plan.SkillCatalog, evalFilter string,
	prior plan.PriorMetrics, thresholds Thresholds, runReq chan<- RunRequest) Model {
	return Model{
		screen:     screenForm,
		form:       newForm(session, cat, evalFilter),
		cat:        cat,
		prior:      prior,
		thresholds: thresholds,
		runReq:     runReq,
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

	case tea.MouseMsg:
		if m.screen == screenForm {
			var action formAction
			m.form, action = m.form.handleMouse(msg)
			switch action {
			case actionCancel:
				return m, tea.Quit
			case actionRun:
				return m.startRun()
			}
			return m, nil
		}
		m.dash.handleMouse(msg)
		return m, nil

	case unitStartedMsg, unitSkippedMsg, itemStartedMsg, baselineStartedMsg, itemDoneMsg,
		baselineDoneMsg, unitFinishedMsg, warnMsg, runDoneMsg:
		m.dash.apply(msg)
		return m, nil
	}
	return m, nil
}

// startRun transitions to the dashboard and dispatches the run request to the
// engine goroutine. The dashboard is built from the canonical plan.Build — the
// same resolver the engine executes — so the tree, ordering, and per-model
// queued/prior state are exactly what will run.
func (m Model) startRun() (tea.Model, tea.Cmd) {
	req := m.form.request()
	p := plan.Build(m.cat, req.Models, req.Selection, m.prior)
	m.dash = newDashboard(p, m.cat, m.prior, m.thresholds)
	m.dash.w, m.dash.h = m.w, m.h
	m.screen = screenDashboard
	return m, tea.Batch(
		func() tea.Msg { m.runReq <- req; return nil },
		m.dash.spin.Tick,
	)
}

func (m Model) View() tea.View {
	content := m.dash.view()
	if m.screen == screenForm {
		content = m.form.view()
	}
	// Alt-screen and mouse mode are declared on the View in bubbletea v2 (the
	// WithAltScreen/WithMouseCellMotion program options are gone); the renderer
	// enters full-window mode and enables click/release/wheel reporting for us.
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
