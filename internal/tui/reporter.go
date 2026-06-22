// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// tuiReporter implements run.Reporter by forwarding every event into the
// bubbletea program. program.Send is goroutine-safe, so the concurrent agent
// workers can report progress directly. The program pointer is set once, after
// the program is constructed, before the engine goroutine starts.
type tuiReporter struct{ p *tea.Program }

var _ run.Reporter = (*tuiReporter)(nil)

// NewReporter returns a run.Reporter that drives the given program.
func NewReporter(p *tea.Program) run.Reporter { return &tuiReporter{p: p} }

// RunDone is the message the engine goroutine sends once every tier finishes.
func RunDone(failed bool, err error) tea.Msg { return runDoneMsg{failed: failed, err: err} }

func (r *tuiReporter) UnitStarted(u plan.UnitRef, total, runs int, mode plan.Mode) {
	r.p.Send(unitStartedMsg{ref: u, total: total, runs: runs, mode: mode})
}

func (r *tuiReporter) UnitSkipped(u plan.UnitRef, reason string) {
	r.p.Send(unitSkippedMsg{ref: u, reason: reason})
}

func (r *tuiReporter) ItemStarted(u plan.UnitRef, item run.ItemStart) {
	r.p.Send(itemStartedMsg{ref: u, item: item})
}

func (r *tuiReporter) ItemDone(u plan.UnitRef, item run.ItemResult) {
	r.p.Send(itemDoneMsg{ref: u, item: item})
}

func (r *tuiReporter) BaselineStarted(u plan.UnitRef, item run.ItemStart) {
	r.p.Send(baselineStartedMsg{ref: u, item: item})
}

func (r *tuiReporter) BaselineDone(u plan.UnitRef, item run.ItemResult) {
	r.p.Send(baselineDoneMsg{ref: u, item: item})
}

func (r *tuiReporter) UnitFinished(u plan.UnitRef, sum run.UnitSummary, savedRel string) {
	r.p.Send(unitFinishedMsg{ref: u, sum: sum, savedRel: savedRel})
}

func (r *tuiReporter) Warn(format string, a ...any) {
	r.p.Send(warnMsg{text: fmt.Sprintf(format, a...)})
}
