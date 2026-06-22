// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package plan

import (
	"testing"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/provider"
)

type fakeProv struct{ name string }

func (p fakeProv) Name() string                         { return p.name }
func (p fakeProv) Display() string                      { return p.name }
func (fakeProv) Models() []provider.Model               { return nil }
func (fakeProv) CLI() []string                          { return []string{"sh"} }
func (fakeProv) EnvKeys() []string                      { return []string{"K"} }
func (fakeProv) SkillDirs() []string                    { return []string{".fake/skills"} }
func (fakeProv) ScanLine([]byte, string) (bool, string) { return false, "" }
func (fakeProv) TriggerSpec(ws, _, _ string, _ bool) provider.CommandSpec {
	return provider.CommandSpec{Argv: []string{"x"}, Dir: ws}
}

func sel(prov, model string) provider.Selection {
	return provider.Selection{Provider: fakeProv{prov}, Model: provider.Model{ID: model, Display: model}}
}

// twoModelCatalog is one plugin/skill with two triggers (q1 should-fire, q2 not)
// and two evals (e1, e2); q2 skips the "skipped" provider.
func twoModelCatalog() []SkillCatalog {
	return []SkillCatalog{{
		Plugin: "p", Skill: "s", Title: "S",
		Triggers: []evalspec.Trigger{
			{Query: "q1", ShouldTrigger: true},
			{Query: "q2", ShouldTrigger: false, SkipProviders: []string{"skipped"}},
		},
		Evals: []evalspec.Eval{{ID: "e1"}, {ID: "e2"}},
	}}
}

func partialSelection(models ...string) Selection {
	s := Selection{Models: map[string]State{}, Cases: map[CaseRef]State{}, Needs: map[string]map[CaseRef]bool{}}
	for _, m := range models {
		s.Models[m] = Partial
		s.Needs[m] = map[CaseRef]bool{}
	}
	return s
}

// TestBuildOrder verifies the plan mirrors the sweep order: plugin → skill →
// model (given order) → all triggers (authored) then all evals (authored).
func TestBuildOrder(t *testing.T) {
	cat := twoModelCatalog()
	models := []provider.Selection{sel("fake", "m1"), sel("fake", "m2")}
	p := Build(cat, models, partialSelection("fake/m1", "fake/m2"), PriorMetrics{})

	if len(p.Plugins) != 1 || p.Plugins[0].Name != "p" {
		t.Fatalf("plugins = %+v, want one plugin p", p.Plugins)
	}
	sk := p.Plugins[0].Skills
	if len(sk) != 1 || sk[0].Skill != "s" {
		t.Fatalf("skills = %+v, want one skill s", sk)
	}
	ms := sk[0].Models
	if len(ms) != 2 || ms[0].Key != "fake/m1" || ms[1].Key != "fake/m2" {
		t.Fatalf("models = %+v, want m1 then m2 (spec order)", ms)
	}
	u := ms[0].Units
	if len(u) != 2 || u[0].Ref.Kind != KindTriggers || u[1].Ref.Kind != KindEvals {
		t.Fatalf("units = %+v, want triggers then evals", u)
	}
	if got := []string{u[0].Cases[0].Label, u[0].Cases[1].Label}; got[0] != "q1" || got[1] != "q2" {
		t.Errorf("trigger order = %v, want [q1 q2] (authored)", got)
	}
	if got := []string{u[1].Cases[0].Label, u[1].Cases[1].Label}; got[0] != "e1" || got[1] != "e2" {
		t.Errorf("eval order = %v, want [e1 e2] (authored)", got)
	}
}

// queuedSet collects the (model,label) pairs the plan marks queued.
func queuedSet(p Plan) map[[2]string]bool {
	out := map[[2]string]bool{}
	for _, pl := range p.Plugins {
		for _, sk := range pl.Skills {
			for _, m := range sk.Models {
				for _, u := range m.Units {
					for _, c := range u.Cases {
						if c.Queued {
							out[[2]string{m.Key, c.Label}] = true
						}
					}
				}
			}
		}
	}
	return out
}

// TestBuildQueuedPerModel pins the merged-filter fix: a case needed only by m1 is
// queued for m1, never for m2 — the resolution is per model.
func TestBuildQueuedPerModel(t *testing.T) {
	cat := twoModelCatalog()
	models := []provider.Selection{sel("fake", "m1"), sel("fake", "m2")}
	s := partialSelection("fake/m1", "fake/m2")
	e1 := CaseRef{Skill: "s", Kind: KindEvals, Case: "e1"}
	s.Needs["fake/m1"][e1] = true // e1 failed only for m1 last run

	q := queuedSet(Build(cat, models, s, PriorMetrics{}))
	if !q[[2]string{"fake/m1", "e1"}] {
		t.Error("e1 must be queued for m1 (it needs it)")
	}
	if q[[2]string{"fake/m2", "e1"}] {
		t.Error("e1 must NOT be queued for m2 (m2 doesn't need it) — the merged-filter bug")
	}
	if len(q) != 1 {
		t.Errorf("queued = %v, want only (m1,e1)", q)
	}
}

// TestBuildCascade pins the form cascade: disabling the only model that needs a
// case unqueues that case everywhere.
func TestBuildCascade(t *testing.T) {
	cat := twoModelCatalog()
	models := []provider.Selection{sel("fake", "m1"), sel("fake", "m2")}
	s := partialSelection("fake/m1", "fake/m2")
	e1 := CaseRef{Skill: "s", Kind: KindEvals, Case: "e1"}
	s.Needs["fake/m1"][e1] = true
	s.Models["fake/m1"] = Off // user disables m1

	if q := queuedSet(Build(cat, models, s, PriorMetrics{})); len(q) != 0 {
		t.Errorf("queued = %v, want nothing once the only needing model is off", q)
	}
}

// TestBuildWiden: turning a case fully On runs it for every enabled model,
// regardless of the needs baseline.
func TestBuildWiden(t *testing.T) {
	cat := twoModelCatalog()
	models := []provider.Selection{sel("fake", "m1"), sel("fake", "m2")}
	s := partialSelection("fake/m1", "fake/m2")
	s.Cases[CaseRef{Skill: "s", Kind: KindEvals, Case: "e2"}] = On

	q := queuedSet(Build(cat, models, s, PriorMetrics{}))
	if !q[[2]string{"fake/m1", "e2"}] || !q[[2]string{"fake/m2", "e2"}] {
		t.Errorf("e2 widened On must run for both models, got %v", q)
	}
}

// TestBuildSkipProvider: a case the provider skips never appears in the plan.
func TestBuildSkipProvider(t *testing.T) {
	cat := twoModelCatalog()
	models := []provider.Selection{sel("skipped", "m1")}
	s := partialSelection("skipped/m1")
	p := Build(cat, models, s, PriorMetrics{})
	for _, c := range p.Plugins[0].Skills[0].Models[0].Units[0].Cases {
		if c.Label == "q2" {
			t.Error("q2 skips the 'skipped' provider and must be absent")
		}
	}
}

// TestFiltersFromQueued: the per-model execution filter contains exactly the
// queued cases.
func TestFiltersFromQueued(t *testing.T) {
	cat := twoModelCatalog()
	models := []provider.Selection{sel("fake", "m1"), sel("fake", "m2")}
	s := partialSelection("fake/m1", "fake/m2")
	s.Needs["fake/m1"][CaseRef{Skill: "s", Kind: KindEvals, Case: "e1"}] = true

	f := Build(cat, models, s, PriorMetrics{}).Filters()
	if _, ok := f["fake/m2"]; ok {
		t.Error("m2 has no queued case; it must be omitted from the filters")
	}
	m1 := f["fake/m1"]
	if m1 == nil || !m1.Evals["s"]["e1"] || m1.Triggers["s"]["q1"] {
		t.Errorf("m1 filter = %+v, want only eval e1 selected", m1)
	}
}
