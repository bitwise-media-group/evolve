// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package plan

import "testing"

func TestFilterInclusion(t *testing.T) {
	var nilF *Filter
	if !nilF.skillIncluded("x") || !nilF.triggerIncluded("x", "q") || !nilF.evalIncluded("x", "e") {
		t.Error("nil filter must include everything")
	}

	f := &Filter{
		Skills:   map[string]bool{"a": true},
		Triggers: map[string]map[string]bool{"a": {"q1": true}},
		Evals:    map[string]map[string]bool{"a": {}}, // present but empty = none
	}
	if !f.skillIncluded("a") || f.skillIncluded("b") {
		t.Error("skillIncluded")
	}
	if !f.triggerIncluded("a", "q1") || f.triggerIncluded("a", "q2") {
		t.Error("triggerIncluded for restricted skill")
	}
	if !f.triggerIncluded("z", "anything") {
		t.Error("triggerIncluded for a skill with no entry must be unrestricted")
	}
	if f.evalIncluded("a", "e1") {
		t.Error("an empty (non-nil) eval set must include nothing")
	}
}
