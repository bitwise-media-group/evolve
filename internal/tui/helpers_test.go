// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/run"
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

func soloCatalog(t *testing.T) []run.SkillCatalog {
	t.Helper()
	return []run.SkillCatalog{{
		Plugin: "solo", Skill: "solo-skill", Title: "Solo", Description: "Does a thing.",
		ResultsDir: t.TempDir(),
		Triggers:   []evalspec.Trigger{{Query: "q1", ShouldTrigger: true}, {Query: "q2", ShouldTrigger: false}},
		Evals: []evalspec.Eval{
			{ID: "e1", Prompt: "do the thing", Files: []evalspec.FileRef{{Rel: "a.txt", Dest: "a.txt"}}},
			{ID: "e2", Prompt: "do another thing"},
		},
	}}
}

func testModel(t *testing.T) Model {
	t.Helper()
	cat := soloCatalog(t)
	sels, m1 := soloModels()
	// Only m1 is resolved; it needs every case (e.g. a plain run).
	needs := map[string]map[run.CaseRef]bool{
		m1.Key(): {
			{Skill: "solo-skill", Kind: run.KindTriggers, Case: "q1"}: true,
			{Skill: "solo-skill", Kind: run.KindTriggers, Case: "q2"}: true,
			{Skill: "solo-skill", Kind: run.KindEvals, Case: "e1"}:    true,
			{Skill: "solo-skill", Kind: run.KindEvals, Case: "e2"}:    true,
		},
	}
	return New(cat, sels, needs, nil, "", make(chan RunRequest, 1))
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
