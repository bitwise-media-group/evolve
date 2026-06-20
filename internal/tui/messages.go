// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// Progress messages mirror the run.Reporter calls; tuiReporter sends them into
// the program from the engine goroutines.
type (
	unitStartedMsg struct {
		ref   run.UnitRef
		total int
		runs  int
		mode  run.Mode
	}
	unitSkippedMsg struct {
		ref    run.UnitRef
		reason string
	}
	itemStartedMsg struct {
		ref  run.UnitRef
		item run.ItemStart
	}
	itemDoneMsg struct {
		ref  run.UnitRef
		item run.ItemResult
	}
	unitFinishedMsg struct {
		ref      run.UnitRef
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

// RunRequest is what the form emits when the user chooses RUN: the models that
// will run and, per model (keyed by Selection.Key()), the filter of cases to
// execute. Per-model filters let a model run a different set of cases than its
// peers — needed so --new reruns only the units that are actually stale.
type RunRequest struct {
	Models  []provider.Selection
	Filters map[string]*run.Filter
}
