// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/run"
)

func TestFormRendersAndPreselects(t *testing.T) {
	m := testModel(t)
	m = step(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	out := m.View()
	if !strings.Contains(out, "Providers") || !strings.Contains(out, "Triggers") || !strings.Contains(out, "Evaluations") {
		t.Errorf("form view missing pane titles:\n%s", out)
	}
	if !m.form.valid() {
		t.Error("form should be valid with the resolved model preselected")
	}
	// All models listed; only the flag-resolved one (m1) is on.
	var leaves, on int
	for _, n := range m.form.left.nodes {
		if n.leaf {
			leaves++
			if n.state == nodeOn {
				on++
			}
		}
	}
	if leaves != 2 || on != 1 {
		t.Errorf("models: %d listed, %d on; want 2 listed, 1 on", leaves, on)
	}

	req := m.form.request()
	if len(req.Models) != 1 || req.Models[0].Model.ID != "m1" {
		t.Fatalf("models = %+v, want only m1", req.Models)
	}
	f := req.Filters[req.Models[0].Key()]
	if f == nil || !f.Triggers["solo-skill"]["q1"] || !f.Evals["solo-skill"]["e1"] {
		t.Errorf("m1 filter did not include the selected cases: %+v", f)
	}
}

func TestFormShowsPreselectionReasons(t *testing.T) {
	cat := soloCatalog(t)
	sels, m1 := soloModels()
	tq1 := run.CaseRef{Skill: "solo-skill", Kind: run.KindTriggers, Case: "q1"}
	// m1 needs only q1, annotated with a reason; q2 is complete and unselected.
	needs := map[string]map[run.CaseRef]bool{m1.Key(): {tq1: true}}
	notes := map[run.CaseRef]string{tq1: "not passing (failed)"}

	m := New(cat, sels, needs, notes, "", make(chan RunRequest, 1))
	m = step(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	out := m.View()
	if !strings.Contains(out, "not passing (failed)") {
		t.Errorf("form view missing the q1 preselection reason:\n%s", out)
	}

	// Toggling q1 off clears its reason text.
	for i := range m.form.triggers.nodes {
		if n := m.form.triggers.nodes[i]; n.leaf && n.caseKey == "q1" {
			m.form.triggers.toggle(i)
		}
	}
	if strings.Contains(m.form.view(), "not passing (failed)") {
		t.Error("reason text should clear after the user toggles the case")
	}
}

// TestPartialSelection is the --new regression: a model needed only for some
// cases must run for only those, not the full cross-product.
func TestPartialSelection(t *testing.T) {
	p := fakeProv{}
	m1 := provider.Selection{Provider: p, Model: provider.Model{ID: "m1"}}
	m2 := provider.Selection{Provider: p, Model: provider.Model{ID: "m2"}}
	cat := []run.SkillCatalog{
		{Plugin: "pl", Skill: "A", Triggers: []evalspec.Trigger{{Query: "a"}}},
		{Plugin: "pl", Skill: "B", Triggers: []evalspec.Trigger{{Query: "b"}}},
	}
	cA := run.CaseRef{Skill: "A", Kind: run.KindTriggers, Case: "a"}
	cB := run.CaseRef{Skill: "B", Kind: run.KindTriggers, Case: "b"}
	needs := map[string]map[run.CaseRef]bool{
		m1.Key(): {cA: true, cB: true},  // m1 needs both
		m2.Key(): {cA: true, cB: false}, // m2 needs only A
	}
	f := newForm(cat, []provider.Selection{m1, m2}, needs, nil, "")

	// Model states: m1 fully on, m2 partial.
	modelState := map[string]nodeState{}
	for _, n := range f.left.nodes {
		if n.leaf {
			modelState[f.sels[n.selIdx].Key()] = n.state
		}
	}
	if modelState[m1.Key()] != nodeOn {
		t.Errorf("m1 state = %v, want on", modelState[m1.Key()])
	}
	if modelState[m2.Key()] != nodePartial {
		t.Errorf("m2 state = %v, want partial", modelState[m2.Key()])
	}

	// Case states: A on (both models need it), B partial (only m1).
	caseState := map[string]nodeState{}
	for _, n := range f.triggers.nodes {
		if n.leaf {
			caseState[n.skill] = n.state
		}
	}
	if caseState["A"] != nodeOn || caseState["B"] != nodePartial {
		t.Errorf("case states = %+v, want A on / B partial", caseState)
	}

	// The crucial part: m2 (partial) runs A but NOT B; m1 runs both.
	req := f.request()
	fm1, fm2 := req.Filters[m1.Key()], req.Filters[m2.Key()]
	if fm1 == nil || !fm1.Triggers["A"]["a"] || !fm1.Triggers["B"]["b"] {
		t.Errorf("m1 filter should run A and B: %+v", fm1)
	}
	if fm2 == nil || !fm2.Triggers["A"]["a"] || fm2.Triggers["B"]["b"] {
		t.Errorf("m2 (partial) must run A but not B: %+v", fm2)
	}
}

// TestSelectingPartialModelRunsAll: toggling a partial model on makes it run
// every selected case.
func TestSelectingPartialModelRunsAll(t *testing.T) {
	p := fakeProv{}
	m1 := provider.Selection{Provider: p, Model: provider.Model{ID: "m1"}}
	m2 := provider.Selection{Provider: p, Model: provider.Model{ID: "m2"}}
	cat := []run.SkillCatalog{
		{Plugin: "pl", Skill: "A", Triggers: []evalspec.Trigger{{Query: "a"}}},
		{Plugin: "pl", Skill: "B", Triggers: []evalspec.Trigger{{Query: "b"}}},
	}
	cA := run.CaseRef{Skill: "A", Kind: run.KindTriggers, Case: "a"}
	cB := run.CaseRef{Skill: "B", Kind: run.KindTriggers, Case: "b"}
	needs := map[string]map[run.CaseRef]bool{
		m1.Key(): {cA: true, cB: true},
		m2.Key(): {cA: true, cB: false},
	}
	f := newForm(cat, []provider.Selection{m1, m2}, needs, nil, "")

	// Toggle the m2 leaf on.
	for i := range f.left.nodes {
		if f.left.nodes[i].leaf && f.sels[f.left.nodes[i].selIdx].Key() == m2.Key() {
			f.left.toggle(i)
		}
	}
	if got := f.request().Filters[m2.Key()]; got == nil || !got.Triggers["B"]["b"] {
		t.Errorf("after selecting m2, it should run B too: %+v", got)
	}
}
