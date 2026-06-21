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
	in := Options{New: true, Failed: true, Modified: true, SkillFilter: []string{"skill"}, Jobs: 4}
	got := in.ClearSelectionFlags()

	if got.New || got.Failed || got.Modified {
		t.Errorf("selection flags not all cleared: %+v", got)
	}
	// Non-selection fields are untouched, and the receiver is not mutated.
	if len(got.SkillFilter) != 1 || got.SkillFilter[0] != "skill" || got.Jobs != 4 {
		t.Errorf("unrelated fields changed: %+v", got)
	}
	if !in.New || !in.Failed || !in.Modified {
		t.Error("receiver mutated; method must return a copy")
	}
}

// TestOptionsSelects pins the --plugin/--skill filter semantics: an empty list
// matches everything, a non-empty list requires membership, and the two filters
// compose with AND.
func TestOptionsSelects(t *testing.T) {
	tests := []struct {
		name          string
		plugins       []string
		skills        []string
		plugin, skill string
		want          bool
	}{
		{"no filters match all", nil, nil, "p", "s", true},
		{"plugin in list", []string{"p", "q"}, nil, "p", "s", true},
		{"plugin not in list", []string{"q"}, nil, "p", "s", false},
		{"skill in list", nil, []string{"s"}, "p", "s", true},
		{"skill not in list", nil, []string{"t"}, "p", "s", false},
		{"both match", []string{"p"}, []string{"s"}, "p", "s", true},
		{"plugin matches but skill does not (AND)", []string{"p"}, []string{"t"}, "p", "s", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := Options{PluginFilter: tc.plugins, SkillFilter: tc.skills}
			if got := o.selects(tc.plugin, tc.skill); got != tc.want {
				t.Errorf("selects(%q, %q) = %v, want %v", tc.plugin, tc.skill, got, tc.want)
			}
		})
	}
}
