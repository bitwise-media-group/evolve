// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package plan

// Plan is the ordered tree of work a sweep will execute, in execution order:
// plugin → skill → model (provider-spec order) → unit (all triggers before all
// evals) → case (authored order). It is the single description of what runs, for
// which models, in what order — built by Build, executed by the engine, and
// rendered by the form (as a live preview) and the dashboard.
type Plan struct {
	Plugins []Plugin
}

// Plugin groups a plugin's skills.
type Plugin struct {
	Name   string
	Skills []Skill
}

// Skill groups one skill's models.
type Skill struct {
	Skill  string
	Title  string
	Models []Model
}

// Model is one provider/model under a skill, carrying its trigger and eval units.
type Model struct {
	Key     string // "provider/model"
	Display string
	Units   []Unit
}

// Unit is one tier (triggers or evals) of a model, with its ordered cases.
type Unit struct {
	Ref   UnitRef
	Cases []Case
}

// Case is one authored trigger query or eval, resolved for its model: Queued
// reports whether it runs this session, and the prior fields carry the last
// committed result (so a non-queued case renders read-only and a queued one tints
// its pending indicator by how it did last time).
type Case struct {
	Label         string // trigger query or eval id
	Kind          Kind
	ShouldTrigger bool // triggers only: expected to fire
	Queued        bool // resolved: will run this session, for this model

	HasPrior    bool
	PriorStatus Status
	Prior       ItemMetrics
}

// Filters derives the per-model execution filter from the plan's queued cases —
// the run set the engine executes, keyed by model. A model with no queued case is
// omitted, so the engine skips it. This is the bridge from the resolved plan back
// to the Filter the sweep consumes, so execution runs exactly what the plan shows.
func (p Plan) Filters() map[string]*Filter {
	out := map[string]*Filter{}
	for _, pl := range p.Plugins {
		for _, sk := range pl.Skills {
			for _, m := range sk.Models {
				for _, u := range m.Units {
					for _, c := range u.Cases {
						if !c.Queued {
							continue
						}
						f := out[m.Key]
						if f == nil {
							f = &Filter{
								Skills:   map[string]bool{},
								Triggers: map[string]map[string]bool{},
								Evals:    map[string]map[string]bool{},
							}
							out[m.Key] = f
						}
						f.Skills[u.Ref.Skill] = true
						dst := f.Triggers
						if c.Kind == KindEvals {
							dst = f.Evals
						}
						if dst[u.Ref.Skill] == nil {
							dst[u.Ref.Skill] = map[string]bool{}
						}
						dst[u.Ref.Skill][c.Label] = true
					}
				}
			}
		}
	}
	return out
}
