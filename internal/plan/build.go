// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package plan

import (
	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/provider"
)

// Build resolves a Selection into the ordered Plan the sweep will execute. The
// order is fixed — plugin → skill → model (the order models were given) → all
// triggers then all evals (authored order within each), the same order the engine
// sweeps — so the form preview, the engine, and the dashboard all agree. Every
// case applicable to a model appears; Queued marks the ones that run this session,
// and each case carries its prior committed result. cat is expected in repository
// order (Catalog returns it plugin-grouped, skill within).
func Build(cat []SkillCatalog, models []provider.Selection, sel Selection, prior PriorMetrics) Plan {
	var p Plan
	for _, sc := range cat {
		sk := buildSkill(sc, models, sel, prior)
		if len(sk.Models) == 0 {
			continue // no model has an applicable case in this skill
		}
		if n := len(p.Plugins); n > 0 && p.Plugins[n-1].Name == sc.Plugin {
			p.Plugins[n-1].Skills = append(p.Plugins[n-1].Skills, sk)
		} else {
			p.Plugins = append(p.Plugins, Plugin{Name: sc.Plugin, Skills: []Skill{sk}})
		}
	}
	return p
}

// buildSkill builds one skill's models, each with its trigger unit then eval unit
// (whichever has applicable cases), in the given model order.
func buildSkill(sc SkillCatalog, models []provider.Selection, sel Selection, prior PriorMetrics) Skill {
	sk := Skill{Skill: sc.Skill, Title: sc.Title}
	for _, m := range models {
		key, prov := m.Key(), m.Provider.Name()
		var units []Unit
		if trig := ApplicableTriggers(sc.Triggers, prov, sc.Skill, nil); len(trig) > 0 {
			ref := UnitRef{Skill: sc.Skill, Key: key, Kind: KindTriggers}
			cases := make([]Case, len(trig))
			for i, t := range trig {
				cases[i] = triggerCase(ref, t, sel, prior)
			}
			units = append(units, Unit{Ref: ref, Cases: cases})
		}
		if evs := ApplicableEvals(sc.Evals, prov, sc.Skill, nil); len(evs) > 0 {
			ref := UnitRef{Skill: sc.Skill, Key: key, Kind: KindEvals}
			cases := make([]Case, len(evs))
			for i, e := range evs {
				cases[i] = evalCase(ref, e.ID, sel, prior)
			}
			units = append(units, Unit{Ref: ref, Cases: cases})
		}
		if len(units) > 0 {
			sk.Models = append(sk.Models, Model{Key: key, Display: modelDisplay(m), Units: units})
		}
	}
	return sk
}

func modelDisplay(m provider.Selection) string {
	if m.Model.Display != "" {
		return m.Model.Display
	}
	return m.Model.ID
}

// triggerCase resolves one trigger query for a model: whether it is queued, and
// its prior committed result.
func triggerCase(ref UnitRef, t evalspec.Trigger, sel Selection, prior PriorMetrics) Case {
	cr := CaseRef{Skill: ref.Skill, Kind: KindTriggers, Case: t.Query}
	c := Case{Label: t.Query, Kind: KindTriggers, ShouldTrigger: t.ShouldTrigger, Queued: sel.queued(ref.Key, cr)}
	if m, ok := prior.TriggerPrevious(ref, t.Query); ok && m.Passed != nil {
		c.HasPrior = true
		c.PriorStatus = boolStatus(*m.Passed)
		c.Prior = ItemMetrics{Hits: m.Hits, Runs: m.Runs, AvgRunSeconds: m.AvgRunSeconds}
		if m.Estimate != nil {
			in := m.Estimate.InputTokens
			c.Prior.InputTokens = &in
			c.Prior.CostUSD = m.Estimate.InputCostUSD
		}
	}
	return c
}

// evalCase resolves one eval for a model: whether it is queued, and its prior
// committed result (a runtime error outranks a pass/fail).
func evalCase(ref UnitRef, id string, sel Selection, prior PriorMetrics) Case {
	cr := CaseRef{Skill: ref.Skill, Kind: KindEvals, Case: id}
	c := Case{Label: id, Kind: KindEvals, Queued: sel.queued(ref.Key, cr)}
	if m, ok := prior.EvalPrevious(ref, id); ok && (m.Passed != nil || m.Errored) {
		c.HasPrior = true
		c.PriorStatus = boolStatus(m.Passed != nil && *m.Passed)
		if m.Errored {
			c.PriorStatus = StatusError
		}
		c.Prior = ItemMetrics{AvgRunSeconds: m.AvgRunSeconds, AssertPassed: m.AssertPassed, AssertTotal: m.AssertTotal}
		if m.Measured != nil {
			c.Prior.InputTokens = m.Measured.InputTokens
			c.Prior.OutputTokens = m.Measured.OutputTokens
			c.Prior.CacheReadTokens = m.Measured.CacheReadTokens
			c.Prior.CacheCreationTokens = m.Measured.CacheCreationTokens
			c.Prior.CostUSD = m.Measured.CostUSD
		}
	}
	return c
}

// boolStatus maps a stored pass/fail bool to its settled status.
func boolStatus(passed bool) Status {
	if passed {
		return StatusPass
	}
	return StatusFail
}
