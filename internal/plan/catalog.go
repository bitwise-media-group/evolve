// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package plan

import "github.com/bitwise-media-group/evolve/internal/evalspec"

// SkillCatalog is one skill's metadata and authored test cases — the data both
// TUI panes draw from. It is the parsed spec, independent of any run.
type SkillCatalog struct {
	Plugin      string
	Skill       string
	Title       string // SKILL.md frontmatter title (falls back to name)
	Description string
	SkillDir    string // the skill's root directory, fingerprinted for --modified
	ResultsDir  string // evals/<skill>, where results.<ext> persists
	Triggers    []evalspec.Trigger
	Evals       []evalspec.Eval
}

// Filter narrows a sweep to specific skills and individual triggers/evals, on
// top of the PluginFilter/SkillFilter/EvalFilter and per-case SkipProviders. A nil *Filter, or a
// nil sub-map, imposes no restriction at that level — so the flag-only path
// (Filter == nil) behaves exactly as before. The TUI selection form populates
// it explicitly: an empty (non-nil) per-skill set means "this skill is included
// but none of its cases", which a missing entry (nil) does not.
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

// ApplicableTriggers is every trigger the model under skill could run: those the
// filter includes and the provider does not skip, in authored order.
func ApplicableTriggers(triggers []evalspec.Trigger, providerName, skill string, f *Filter) []evalspec.Trigger {
	if !f.skillIncluded(skill) {
		return nil
	}
	var out []evalspec.Trigger
	for _, t := range triggers {
		if t.SkipsProvider(providerName) {
			continue
		}
		if !f.triggerIncluded(skill, t.Query) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// ApplicableEvals is every eval the model under skill could run: those the filter
// includes and the provider does not skip, in authored order.
func ApplicableEvals(evals []evalspec.Eval, providerName, skill string, f *Filter) []evalspec.Eval {
	if !f.skillIncluded(skill) {
		return nil
	}
	var out []evalspec.Eval
	for _, c := range evals {
		if c.SkipsProvider(providerName) {
			continue
		}
		if !f.evalIncluded(skill, c.ID) {
			continue
		}
		out = append(out, c)
	}
	return out
}
