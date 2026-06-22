// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/provider"
)

type fakeProv struct{}

func (fakeProv) Name() string                           { return "fake" }
func (fakeProv) Display() string                        { return "Fake" }
func (fakeProv) Models() []provider.Model               { return []provider.Model{{ID: "m1", Display: "Model 1"}} }
func (fakeProv) CLI() []string                          { return []string{"sh"} }
func (fakeProv) EnvKeys() []string                      { return []string{"K"} }
func (fakeProv) SkillDirs() []string                    { return []string{".fake/skills"} }
func (fakeProv) ScanLine([]byte, string) (bool, string) { return false, "" }
func (fakeProv) TriggerSpec(ws, query, model string, _ bool) provider.CommandSpec {
	return provider.CommandSpec{Argv: []string{"x"}, Dir: ws}
}

// soloModels returns two models of which only the first is flag-resolved.
func soloModels() (sels []provider.Selection, resolved provider.Selection) {
	p := fakeProv{}
	sels = []provider.Selection{
		{Provider: p, Model: provider.Model{ID: "m1", Display: "Model 1"}},
		{Provider: p, Model: provider.Model{ID: "m2", Display: "Model 2"}},
	}
	return sels, sels[0]
}

func soloCatalog(t *testing.T) []plan.SkillCatalog {
	t.Helper()
	return []plan.SkillCatalog{{
		Plugin: "solo", Skill: "solo-skill", Title: "Solo", Description: "Does a thing.",
		ResultsDir: t.TempDir(),
		Triggers:   []evalspec.Trigger{{Query: "q1", ShouldTrigger: true}, {Query: "q2", ShouldTrigger: false}},
		Evals: []evalspec.Eval{
			{ID: "e1", Prompt: "do the thing", Files: []evalspec.FileRef{{Rel: "a.txt", Dest: "a.txt"}}},
			{ID: "e2", Prompt: "do another thing"},
		},
	}}
}

// evalOnlyCatalog is one skill with two evals and no triggers, for tests that
// build an evals-only dashboard (so the plan has no trigger rows to count).
func evalOnlyCatalog(t *testing.T) []plan.SkillCatalog {
	t.Helper()
	return []plan.SkillCatalog{{
		Plugin: "solo", Skill: "solo-skill", Title: "Solo",
		ResultsDir: t.TempDir(),
		Evals:      []evalspec.Eval{{ID: "e1"}, {ID: "e2"}},
	}}
}

func testModel(t *testing.T) Model {
	t.Helper()
	cat := soloCatalog(t)
	sels, m1 := soloModels()
	// Only m1 is resolved; it needs every case (e.g. a plain run).
	needs := map[string]map[plan.CaseRef]bool{
		m1.Key(): {
			{Skill: "solo-skill", Kind: plan.KindTriggers, Case: "q1"}: true,
			{Skill: "solo-skill", Kind: plan.KindTriggers, Case: "q2"}: true,
			{Skill: "solo-skill", Kind: plan.KindEvals, Case: "e1"}:    true,
			{Skill: "solo-skill", Kind: plan.KindEvals, Case: "e2"}:    true,
		},
	}
	return New(cat, sels, needs, nil, "", plan.PriorMetrics{}, make(chan RunRequest, 1))
}

// selectionFromFilter builds the plan.Selection the dashboard would receive for
// the given models and case filter: a nil filter widens every model (run all
// applicable cases), otherwise each model's needs baseline is the filter's
// selected cases. It mirrors what the form's request() produces.
func selectionFromFilter(models []provider.Selection, filter *plan.Filter) plan.Selection {
	sel := plan.Selection{
		Models: map[string]plan.State{},
		Cases:  map[plan.CaseRef]plan.State{},
		Needs:  map[string]map[plan.CaseRef]bool{},
	}
	for _, m := range models {
		k := m.Key()
		if filter == nil {
			sel.Models[k] = plan.On // widen: every applicable case runs
			continue
		}
		sel.Models[k] = plan.Partial
		needs := map[plan.CaseRef]bool{}
		for skill, qs := range filter.Triggers {
			for q, on := range qs {
				if on {
					needs[plan.CaseRef{Skill: skill, Kind: plan.KindTriggers, Case: q}] = true
				}
			}
		}
		for skill, es := range filter.Evals {
			for e, on := range es {
				if on {
					needs[plan.CaseRef{Skill: skill, Kind: plan.KindEvals, Case: e}] = true
				}
			}
		}
		sel.Needs[k] = needs
	}
	return sel
}

// dashFromFilter builds a dashboard the way the app does: resolve the models and
// case filter into a plan via plan.Build, then project it.
func dashFromFilter(cat []plan.SkillCatalog, models []provider.Selection, filter *plan.Filter, prior plan.PriorMetrics) dashboardModel {
	p := plan.Build(cat, models, selectionFromFilter(models, filter), prior)
	return newDashboard(p, cat, prior)
}

// runeKey builds a printable key-press message from a single-character string,
// the bubbletea v2 replacement for tea.KeyMsg{Type: tea.KeyRunes, ...}.
func runeKey(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func step(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func stepCmd(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func caseNodes(nodes []nodeRef) int {
	n := 0
	for _, nd := range nodes {
		if nd.kind == nkCase {
			n++
		}
	}
	return n
}
