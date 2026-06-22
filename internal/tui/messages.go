// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// Progress messages mirror the run.Reporter calls; tuiReporter sends them into
// the program from the engine goroutines.
type (
	unitStartedMsg struct {
		ref   plan.UnitRef
		total int
		runs  int
		mode  plan.Mode
	}
	unitSkippedMsg struct {
		ref    plan.UnitRef
		reason string
	}
	itemStartedMsg struct {
		ref  plan.UnitRef
		item run.ItemStart
	}
	baselineStartedMsg struct {
		ref  plan.UnitRef
		item run.ItemStart
	}
	itemDoneMsg struct {
		ref  plan.UnitRef
		item run.ItemResult
	}
	baselineDoneMsg struct {
		ref  plan.UnitRef
		item run.ItemResult
	}
	unitFinishedMsg struct {
		ref      plan.UnitRef
		sum      run.UnitSummary
		savedRel string
	}
	warnMsg struct{ text string }

	// runDoneMsg arrives once the engine goroutine has finished every tier.
	runDoneMsg struct {
		failed bool
		err    error
	}
)

// RunRequest is what the form emits when the user chooses RUN: the models the run
// spans (in display/spec order) and the resolved Selection (enable/disable intent
// plus the preselect baseline). The engine builds the canonical plan.Plan from
// these via plan.Build — the single resolver the form preview also uses — so what
// runs cannot drift from what the form showed.
type RunRequest struct {
	Models    []provider.Selection
	Selection plan.Selection
}
