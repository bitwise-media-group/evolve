// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package plan

import (
	"testing"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/model"
)

// sessionFixture builds a one-skill catalog with two triggers and one eval, two
// models on distinct harnesses, and per-model reasons exercising new/modified/
// failing.
func sessionFixture() (cat []SkillCatalog, models []model.Model, harnesses []HarnessState, reasons Reasons) {
	cat = []SkillCatalog{{
		Plugin: "p", Skill: "s", Title: "S",
		Triggers: []evalspec.Trigger{{Query: "q1"}, {Query: "q2"}},
		Evals:    []evalspec.Eval{{ID: "e1"}},
	}}
	models = []model.Model{
		{ID: "v/m1", ProviderID: "v", Name: "M1", Supported: map[string]string{"h1": "m1"}, Preferred: "h1"},
		{ID: "v/m2", ProviderID: "v", Name: "M2", Supported: map[string]string{"h2": "m2"}, Preferred: "h2"},
	}
	harnesses = []HarnessState{
		{Harness: fakeHarness{"h1"}, Available: true},
		{Harness: fakeHarness{"h2"}, Available: true},
	}
	q1 := CaseRef{Skill: "s", Kind: KindTriggers, Case: "q1"}
	q2 := CaseRef{Skill: "s", Kind: KindTriggers, Case: "q2"}
	e1 := CaseRef{Skill: "s", Kind: KindEvals, Case: "e1"}
	reasons = Reasons{
		"v/m1": {q1: {New: true}, q2: {Failing: true}, e1: {}},
		"v/m2": {q1: {New: true}, q2: {}, e1: {Modified: true}},
	}
	return cat, models, harnesses, reasons
}

func newFixtureSession(filters Filters) *Session {
	cat, models, harnesses, reasons := sessionFixture()
	return NewSession(cat, models, harnesses, PriorMetrics{}, reasons, filters,
		[]string{"h1", "h2"}, []string{"v/m1", "v/m2"})
}

func TestSessionNoFilterRunsAll(t *testing.T) {
	s := newFixtureSession(Filters{})
	if got := len(queuedSet(s.Plan())); got != 6 {
		t.Errorf("no filter queued = %d, want 6 (2 models × 3 cases)", got)
	}
	if !s.AnyQueued() {
		t.Error("AnyQueued = false, want true")
	}
}

func TestSessionFilters(t *testing.T) {
	q1m1 := [2]string{"v/m1", "q1"}
	q2m1 := [2]string{"v/m1", "q2"}
	q1m2 := [2]string{"v/m2", "q1"}
	e1m2 := [2]string{"v/m2", "e1"}

	t.Run("new only", func(t *testing.T) {
		q := queuedSet(newFixtureSession(Filters{New: true}).Plan())
		if !q[q1m1] || !q[q1m2] || len(q) != 2 {
			t.Errorf("new filter queued = %v, want only the two new q1s", q)
		}
	})
	t.Run("failed only", func(t *testing.T) {
		q := queuedSet(newFixtureSession(Filters{Failed: true}).Plan())
		if !q[q2m1] || len(q) != 1 {
			t.Errorf("failed filter queued = %v, want only m1/q2", q)
		}
	})
	t.Run("new and modified compose", func(t *testing.T) {
		q := queuedSet(newFixtureSession(Filters{New: true, Modified: true}).Plan())
		if !q[q1m1] || !q[q1m2] || !q[e1m2] || len(q) != 3 {
			t.Errorf("new+modified queued = %v, want m1/q1, m2/q1, m2/e1", q)
		}
	})
}

func TestSessionEnableModel(t *testing.T) {
	s := newFixtureSession(Filters{})
	s.EnableModel("v/m2", false)
	q := queuedSet(s.Plan())
	if len(q) != 3 {
		t.Fatalf("after disabling m2, queued = %v, want 3 (m1 only)", q)
	}
	for k := range q {
		if k[0] != "v/m1" {
			t.Errorf("disabled m2 still queued: %v", k)
		}
	}
}

func TestSessionEnableHarness(t *testing.T) {
	s := newFixtureSession(Filters{})
	s.EnableHarness("h1", false) // m1 only runs on h1
	q := queuedSet(s.Plan())
	if len(q) != 3 {
		t.Fatalf("after disabling h1, queued = %v, want 3 (m2 only)", q)
	}
	for k := range q {
		if k[0] != "v/m2" {
			t.Errorf("m1 (h1) should be dropped, got %v", k)
		}
	}
	// m1 is no longer runnable; m2 still is.
	if s.ModelRunnable(s.models[0]) {
		t.Error("m1 should not be runnable with h1 disabled")
	}
	if !s.ModelRunnable(s.models[1]) {
		t.Error("m2 should still be runnable")
	}
}

// offeredFixtureSession builds the standard fixture with an offered-models
// probe result on h1: only "m1-other" is offered, so m1 (cli id "m1") is not.
func offeredFixtureSession(h1Models []string) *Session {
	cat, models, harnesses, reasons := sessionFixture()
	harnesses[0].Models = h1Models
	return NewSession(cat, models, harnesses, PriorMetrics{}, reasons, Filters{},
		[]string{"h1", "h2"}, []string{"v/m1", "v/m2"})
}

func TestSessionOfferedModelsDeselects(t *testing.T) {
	s := offeredFixtureSession([]string{"m1-other"})
	if s.ModelEnabled("v/m1") {
		t.Error("m1 is not offered by h1's CLI; it should start deselected")
	}
	if !s.ModelEnabled("v/m2") {
		t.Error("m2's harness has no probe result (unknown); it should stay selected")
	}
	if s.ModelOffered(s.models[0]) {
		t.Error("ModelOffered(m1) = true, want false")
	}
	if !s.ModelOffered(s.models[1]) {
		t.Error("ModelOffered(m2) = false, want true (unknown fails open)")
	}
	// Deselected is not unselectable: the user can still force it on.
	s.EnableModel("v/m1", true)
	if !s.ModelEnabled("v/m1") {
		t.Error("an unoffered model must stay toggleable")
	}
}

func TestSessionOfferedModelsMatch(t *testing.T) {
	for _, tokens := range [][]string{{"m1"}, {"M1"}} {
		s := offeredFixtureSession(tokens)
		if !s.ModelEnabled("v/m1") {
			t.Errorf("m1 offered as %v; it should stay selected", tokens)
		}
	}
}

func TestSessionOfferedIgnoredWhenNotRunnable(t *testing.T) {
	s := offeredFixtureSession([]string{"m1-other"})
	s.EnableHarness("h1", false)
	// With h1 disabled m1 has no harness at all; ModelOffered defers to the
	// ModelRunnable gate rather than double-reporting.
	if !s.ModelOffered(s.models[0]) {
		t.Error("ModelOffered = false for an unrunnable model, want true (uns. path covers it)")
	}
}

func TestSessionForceCases(t *testing.T) {
	s := newFixtureSession(Filters{New: true}) // baseline queues only q1s
	q2 := CaseRef{Skill: "s", Kind: KindTriggers, Case: "q2"}

	// Force q2 on: it runs for every enabled model regardless of the filter.
	s.SetCases([]CaseRef{q2}, On)
	if got := s.NodeSel([]CaseRef{q2}); got != SelForceOn {
		t.Errorf("q2 = %v, want SelForceOn", got)
	}
	q := queuedSet(s.Plan())
	if !q[[2]string{"v/m1", "q2"}] || !q[[2]string{"v/m2", "q2"}] {
		t.Errorf("forced-on q2 should run for both models: %v", q)
	}

	// Force q1 off: it stops running despite matching the new filter.
	q1 := CaseRef{Skill: "s", Kind: KindTriggers, Case: "q1"}
	s.SetCases([]CaseRef{q1}, Off)
	if got := s.NodeSel([]CaseRef{q1}); got != SelForceOff {
		t.Errorf("q1 = %v, want SelForceOff", got)
	}
	if queuedSet(s.Plan())[[2]string{"v/m1", "q1"}] {
		t.Error("forced-off q1 should not run")
	}
}

func TestSessionNodeSelAndAuto(t *testing.T) {
	q1 := CaseRef{Skill: "s", Kind: KindTriggers, Case: "q1"}
	e1 := CaseRef{Skill: "s", Kind: KindEvals, Case: "e1"}

	// No filter: q1 is queued for both models -> auto all.
	noFilter := newFixtureSession(Filters{})
	if got := noFilter.NodeSel([]CaseRef{q1}); got != SelAutoAll {
		t.Errorf("q1 (no filter) = %v, want SelAutoAll", got)
	}

	// New filter: e1 is new for neither model -> auto none, and not available.
	newOnly := newFixtureSession(Filters{New: true})
	if got := newOnly.NodeSel([]CaseRef{e1}); got != SelAutoNone {
		t.Errorf("e1 (new filter) = %v, want SelAutoNone", got)
	}
	if newOnly.AutoAvailable([]CaseRef{e1}) {
		t.Error("e1 should have no auto-queued pair under the new filter")
	}

	// Modified filter: e1 is modified for m2 only -> auto partial.
	mod := newFixtureSession(Filters{Modified: true})
	if got := mod.NodeSel([]CaseRef{e1}); got != SelAutoPartial {
		t.Errorf("e1 (modified filter) = %v, want SelAutoPartial", got)
	}
}
