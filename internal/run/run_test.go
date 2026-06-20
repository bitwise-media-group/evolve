// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import "testing"

// TestClearSelectionFlags pins the invariant the TUI relies on: once the form
// encodes the user's choice as an explicit per-model Filter, every selection
// flag must be cleared so the engine runs the selection verbatim. A new
// selection flag added without being cleared here would silently re-filter the
// form's picks (the bug that dropped --modified cases in the TUI).
func TestClearSelectionFlags(t *testing.T) {
	in := Options{New: true, Failed: true, Modified: true, SkillFilter: "skill", Jobs: 4}
	got := in.ClearSelectionFlags()

	if got.New || got.Failed || got.Modified {
		t.Errorf("selection flags not all cleared: %+v", got)
	}
	// Non-selection fields are untouched, and the receiver is not mutated.
	if got.SkillFilter != "skill" || got.Jobs != 4 {
		t.Errorf("unrelated fields changed: %+v", got)
	}
	if !in.New || !in.Failed || !in.Modified {
		t.Error("receiver mutated; method must return a copy")
	}
}
