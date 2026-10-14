// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package plan

import (
	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/model"
)

// SkillCatalog is one skill's metadata and authored test cases — the data both
// TUI panes draw from. It is the parsed spec, independent of any run.
type SkillCatalog struct {
	Plugin      string
	Skill       string
	Title       string // SKILL.md frontmatter title (falls back to name)
	Description string
	SkillDir    string   // the skill's root directory, fingerprinted for --modified
	ResultsDir  string   // evals/<skill>, where results.<ext> persists
	Agent       bool     // a hosted agent's suite (Skill is the agent name, SkillDir its directory)
	Models      []string // eval-set models restriction (intersected with the root models); empty = all
	Triggers    []evalspec.Trigger
	Evals       []evalspec.Eval
}

// Filter narrows a sweep to specific skills and individual triggers/evals, on
// top of the PluginFilter/SkillFilter/EvalFilter and the eval-set models
// restriction. A nil *Filter, or a nil sub-map, imposes no restriction at that
// level — so the flag-only path (Filter == nil) behaves exactly as before. The
// TUI selection form populates it explicitly: an empty (non-nil) per-skill set
// means "this skill is included but none of its cases", which a missing entry
// (nil) does not.
type Filter struct {
	Skills   map[string]bool            // nil = all skills
	Triggers map[string]map[string]bool // skill -> selected trigger queries
	Evals    map[string]map[string]bool // skill -> selected eval ids
}

func (f *Filter) skillIncluded(skill string) bool {
	if f == nil || f.Skills == nil {
		return true
	}
	return f.Skills[skill]
}

func (f *Filter) triggerIncluded(skill, query string) bool {
	if f == nil || f.Triggers == nil {
		return true
	}
	sub, ok := f.Triggers[skill]
	if !ok {
		return true
	}
	return sub[query]
}

func (f *Filter) evalIncluded(skill, id string) bool {
	if f == nil || f.Evals == nil {
		return true
	}
	sub, ok := f.Evals[skill]
	if !ok {
		return true
	}
	return sub[id]
}

// modelAllowed reports whether m is within a skill's eval-set models
// restriction. An empty restriction (the common case) allows every model; a
// non-empty one allows only the models its tokens name — the intersection with
// the root models, since m already comes from the root-resolved set.
func modelAllowed(m model.Model, allowedModels []string) bool {
	return len(allowedModels) == 0 || m.MatchedBy(allowedModels)
}

// Allows reports whether model m may run this skill's cases: m is of the
// set's kind (a hosted endpoint for an agent, a registry model for a skill)
// and within its restriction (eval-set models, or an agent's endpoints). It is
// the per-skill gate the engine's case-by-case previews share with
// ApplicableTriggers/ApplicableEvals.
func (sc SkillCatalog) Allows(m model.Model) bool {
	return sc.Agent == model.IsHostedProvider(m.ProviderID) && modelAllowed(m, sc.Models)
}

// ApplicableTriggers is every trigger model m under skill could run: those the
// filter includes, in authored order, and none when m is outside the eval-set
// models restriction.
func ApplicableTriggers(
	triggers []evalspec.Trigger, m model.Model, allowedModels []string, skill string, f *Filter,
) []evalspec.Trigger {
	if !f.skillIncluded(skill) || !modelAllowed(m, allowedModels) {
		return nil
	}
	var out []evalspec.Trigger
	for _, t := range triggers {
		if !f.triggerIncluded(skill, t.Query) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// ApplicableEvals is every eval model m under skill could run: those the filter
// includes, in authored order, and none when m is outside the eval-set models
// restriction.
func ApplicableEvals(
	evals []evalspec.Eval, m model.Model, allowedModels []string, skill string, f *Filter,
) []evalspec.Eval {
	if !f.skillIncluded(skill) || !modelAllowed(m, allowedModels) {
		return nil
	}
	var out []evalspec.Eval
	for _, c := range evals {
		if !f.evalIncluded(skill, c.ID) {
			continue
		}
		out = append(out, c)
	}
	return out
}
