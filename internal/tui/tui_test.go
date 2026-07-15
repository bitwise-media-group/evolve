// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/plan"
)

type fakeProv struct{}

func (fakeProv) ID() string                             { return "fake" }
func (fakeProv) Name() string                           { return "Fake" }
func (fakeProv) CLI() []string                          { return []string{"sh"} }
func (fakeProv) EnvKeys() []string                      { return []string{"K"} }
func (fakeProv) SkillDirs() []string                    { return []string{".fake/skills"} }
func (fakeProv) ScanLine([]byte, string, string) (bool, string) { return false, "" }
func (fakeProv) TriggerSpec(ws, query, cliModelID string, _ bool) model.CommandSpec {
	return model.CommandSpec{Argv: []string{"x"}, Dir: ws}
}

// fakeModel builds a canonical model driven by the fake harness.
func fakeModel(id, name string) model.Model {
	return model.Model{
		ID: "fake/" + id, ProviderID: "fake", Name: name,
		Supported: map[string]string{"fake": id}, Preferred: "fake",
	}
}

// soloModels returns two models of which only the first is flag-resolved.
func soloModels() (sels []harness.Selection, resolved harness.Selection) {
	p := fakeProv{}
	sels = []harness.Selection{
		{Harness: p, Model: fakeModel("m1", "Model 1")},
		{Harness: p, Model: fakeModel("m2", "Model 2")},
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

// manySkillCatalog is n evals-only skills under one plugin, titled "Sk00".."SkNN",
// for tests that need the Rollup pane's Skills tab to overflow a short pane so its
// rows scroll.
func manySkillCatalog(t *testing.T, n int) []plan.SkillCatalog {
	t.Helper()
	cat := make([]plan.SkillCatalog, n)
	for i := range cat {
		cat[i] = plan.SkillCatalog{
			Plugin: "solo", Skill: fmt.Sprintf("skill-%02d", i), Title: fmt.Sprintf("Sk%02d", i),
			ResultsDir: t.TempDir(),
			Evals:      []evalspec.Eval{{ID: "e1"}, {ID: "e2"}},
		}
	}
	return cat
}

// testSession builds a form session over cat with the fake harness available,
// both models known, the given models enabled, and the given filters. Every case
// is categorized "new" so, with no filter set, all are queued (a plain run).
func testSession(t *testing.T, cat []plan.SkillCatalog, enabledModelKeys []string, filters plan.Filters) *plan.Session {
	t.Helper()
	models := []model.Model{fakeModel("m1", "Model 1"), fakeModel("m2", "Model 2")}
	harnesses := []plan.HarnessState{{Harness: fakeProv{}, Available: true}}
	reasons := plan.Reasons{}
	for _, m := range models {
		rm := map[plan.CaseRef]plan.CaseReason{}
		for _, sc := range cat {
			for _, tr := range sc.Triggers {
				rm[plan.CaseRef{Skill: sc.Skill, Kind: plan.KindTriggers, Case: tr.Query}] = plan.CaseReason{New: true}
			}
			for _, ev := range sc.Evals {
				rm[plan.CaseRef{Skill: sc.Skill, Kind: plan.KindEvals, Case: ev.ID}] = plan.CaseReason{New: true}
			}
		}
		reasons[m.Key()] = rm
	}
	return plan.NewSession(cat, models, harnesses, plan.PriorMetrics{}, reasons, filters, []string{"fake"}, enabledModelKeys)
}

// testThresholds mirrors the built-in report defaults; literal values keep the
// presentation package decoupled from internal/report (the report package's
// tests pin the constants themselves).
var testThresholds = Thresholds{Triggers: 0.5, Evals: 0.66}

func testModel(t *testing.T) Model {
	t.Helper()
	cat := soloCatalog(t)
	// Only m1 is enabled; with no filter set it needs every case (a plain run).
	session := testSession(t, cat, []string{"fake/m1"}, plan.Filters{})
	return New(session, cat, "", plan.PriorMetrics{}, testThresholds, make(chan RunRequest, 1))
}

// selectionFromFilter builds the plan.Selection the dashboard would receive for
// the given models and case filter: a nil filter widens every model (run all
// applicable cases), otherwise each model's needs baseline is the filter's
// selected cases. It mirrors what the form's request() produces.
func selectionFromFilter(models []harness.Selection, filter *plan.Filter) plan.Selection {
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
func dashFromFilter(cat []plan.SkillCatalog, models []harness.Selection, filter *plan.Filter, prior plan.PriorMetrics) dashboardModel {
	p := plan.Build(cat, models, selectionFromFilter(models, filter), prior)
	return newDashboard(p, cat, prior, testThresholds)
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
