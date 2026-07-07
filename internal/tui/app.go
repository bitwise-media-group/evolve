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

// repaint chains a full-screen redraw onto an update's own commands. It works
// around a rendering bug in bubbletea v2.0.8's ultraviolet renderer: after its
// DECSTBM hard-scroll optimization shifts a region of the terminal, rows inside
// that region whose content did not change since the previous frame are skipped
// by the repaint loop (the scroll marks the physical-screen buffer touched, but
// the loop only consults the app buffer's per-line dirty flags), leaving stale
// duplicate rows on screen. tea.ClearScreen bypasses the scroll optimization
// for the next flush, and painting is ticker-driven, so the buggy diff normally
// never reaches the terminal. Applied to every message that can shift lines
// vertically — user input and engine progress — but not to spinner ticks, which
// only rewrite cells in place. Drop this once the upstream fix lands:
// https://github.com/charmbracelet/ultraviolet/issues/137
func repaint(cmds ...tea.Cmd) tea.Cmd {
	return tea.Batch(append(cmds, tea.ClearScreen)...)
}

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
			return m, repaint()
		}
		if m.dash.handleKey(msg) {
			return m, tea.Quit
		}
		return m, repaint()

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
			return m, repaint()
		}
		m.dash.handleMouse(msg)
		return m, repaint()

	case unitStartedMsg, unitSkippedMsg, itemStartedMsg, baselineStartedMsg, itemDoneMsg,
		baselineDoneMsg, unitFinishedMsg, warnMsg, runDoneMsg:
		m.dash.apply(msg)
		return m, repaint()
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
	// The form→dashboard switch replaces every line, so it repaints too.
	return m, repaint(
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
