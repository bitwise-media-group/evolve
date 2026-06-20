// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"path/filepath"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/manifest"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/results"
)

// Tiers selects which eval tiers a plan covers: a single-tier command enables
// one, `run all` enables both.
type Tiers struct {
	Triggers bool
	Evals    bool
}

// Filter narrows a sweep to specific skills and individual triggers/evals, on
// top of SkillFilter/EvalFilter and per-case SkipProviders. A nil *Filter, or a
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

// SkillCatalog is one skill's metadata and authored test cases — the data both
// TUI panes draw from. It is the parsed spec, independent of any run.
type SkillCatalog struct {
	Plugin      string
	Skill       string
	Title       string // SKILL.md frontmatter title (falls back to name)
	Description string
	ResultsDir  string // evals/<skill>, where results.<ext> persists
	Triggers    []evalspec.Trigger
	Evals       []evalspec.Eval
}

// Catalog loads every skill's triggers, evals, and SKILL.md metadata across the
// repository. It ignores SkillFilter/EvalFilter so the form can show the full
// tree and merely preselect the flag-narrowed subset. A skill whose spec fails
// to parse is included with whatever loaded (so the UI still lists it).
func Catalog(opts Options) ([]SkillCatalog, error) {
	sets, err := opts.Repo.EvalSets()
	if err != nil {
		return nil, err
	}
	cat := make([]SkillCatalog, 0, len(sets))
	for _, set := range sets {
		sc := SkillCatalog{Plugin: set.Plugin.Name, Skill: set.Skill, ResultsDir: set.ResultsDir}
		if fields, ok, _ := manifest.Frontmatter(filepath.Join(set.SkillDir, "SKILL.md")); ok {
			if sc.Title = fields["title"]; sc.Title == "" {
				sc.Title = fields["name"]
			}
			sc.Description = fields["description"]
		}
		if set.TriggersPath != "" {
			if tf, err := evalspec.LoadTriggers(set.TriggersPath); err == nil {
				sc.Triggers = tf.Triggers
			}
		}
		if set.EvalsPath != "" {
			if ef, err := evalspec.LoadEvals(set.EvalsPath); err == nil {
				sc.Evals = ef.Evals
			}
		}
		cat = append(cat, sc)
	}
	return cat, nil
}

// Plan enumerates the execution units a sweep would produce for the given
// selections, tiers, and filter — every (skill, provider/model, tier) triple
// with at least one applicable case. It reuses the engine's applicability
// checks so the planned list cannot drift from what the engine runs.
func Plan(cat []SkillCatalog, sels []provider.Selection, f *Filter, tiers Tiers) []UnitRef {
	var units []UnitRef
	for _, sc := range cat {
		for _, sel := range sels {
			if tiers.Triggers && len(applicableTriggers(sc.Triggers, sel.Provider.Name(), sc.Skill, f)) > 0 {
				units = append(units, UnitRef{Skill: sc.Skill, Key: sel.Key(), Kind: KindTriggers})
			}
			if tiers.Evals && len(applicableEvals(sc.Evals, sel.Provider.Name(), sc.Skill, f)) > 0 {
				units = append(units, UnitRef{Skill: sc.Skill, Key: sel.Key(), Kind: KindEvals})
			}
		}
	}
	return units
}

// PlanFor enumerates the units one selection would run under a per-model filter.
func PlanFor(cat []SkillCatalog, sel provider.Selection, f *Filter, tiers Tiers) []UnitRef {
	return Plan(cat, []provider.Selection{sel}, f, tiers)
}

// Target identifies a (skill, tier) execution unit independent of any model.
type Target struct {
	Skill string
	Kind  Kind
}

// Needs reports, per resolved selection (keyed by Selection.Key()) and per
// in-play target, whether the engine would run that unit. With --new only
// incomplete units are true; otherwise every applicable unit is true. Only
// targets honored by def (the command's default tiers), SkillFilter, and
// evalFilter, and with applicable cases for the model, appear. The TUI derives
// the form's initial tri-state selection from this so it matches non-TUI mode
// exactly. The --new check reuses the engine's own skip logic with a
// conservative always-rerun probe, so it needs no token-counting round trip.
func Needs(
	opts Options, cat []SkillCatalog, sels []provider.Selection, def Tiers, evalFilter string,
) map[string]map[Target]bool {
	out := make(map[string]map[Target]bool, len(sels))
	for _, sel := range sels {
		out[sel.Key()] = map[Target]bool{}
	}
	for _, sc := range cat {
		if opts.SkillFilter != "" && sc.Skill != opts.SkillFilter {
			continue
		}
		var file *results.File
		if opts.New {
			file, _ = results.LoadDir(sc.ResultsDir, sc.Plugin, sc.Skill)
		}
		for _, sel := range sels {
			if def.Triggers {
				if app := applicableTriggers(sc.Triggers, sel.Provider.Name(), sc.Skill, nil); len(app) > 0 {
					out[sel.Key()][Target{Skill: sc.Skill, Kind: KindTriggers}] = triggerUnitNeeds(opts, file, sel, app)
				}
			}
			if def.Evals {
				ef := evalOnlyFilter(sc.Skill, evalFilter)
				if app := applicableEvals(sc.Evals, sel.Provider.Name(), sc.Skill, ef); len(app) > 0 {
					out[sel.Key()][Target{Skill: sc.Skill, Kind: KindEvals}] = evalUnitNeeds(opts, file, sel, app)
				}
			}
		}
	}
	return out
}

// evalOnlyFilter restricts a skill's evals to a single id, mirroring --eval.
func evalOnlyFilter(skill, evalFilter string) *Filter {
	if evalFilter == "" {
		return nil
	}
	return &Filter{Evals: map[string]map[string]bool{skill: {evalFilter: true}}}
}

func alwaysRerunTrigger(evalspec.Trigger) bool { return true }
func alwaysRerunEval(evalspec.Eval) bool       { return true }

// triggerUnitNeeds reports whether the (model, skill, triggers) unit would run.
func triggerUnitNeeds(opts Options, file *results.File, sel provider.Selection, app []evalspec.Trigger) bool {
	if !opts.New {
		return true
	}
	_, cliFound := provider.ResolveCLI(sel.Provider)
	execute := !opts.CountOnly && cliFound
	_, countCapable := sel.Provider.(provider.TokenCounter)
	var entry *results.TriggerEntry
	if file != nil {
		entry = file.Triggers[sel.Key()]
	}
	return triggerSkipReason(entry, app, sel.Model, execute, countCapable, alwaysRerunTrigger) == ""
}

// evalUnitNeeds reports whether the (model, skill, evals) unit would run.
func evalUnitNeeds(opts Options, file *results.File, sel provider.Selection, app []evalspec.Eval) bool {
	if !opts.New {
		return true
	}
	evalRunner, isEvalRunner := sel.Provider.(provider.EvalRunner)
	_, cliFound := provider.ResolveCLI(sel.Provider)
	execute := isEvalRunner && cliFound && !opts.CountOnly
	reportsUsage := isEvalRunner && evalRunner.ReportsUsage()
	_, countCapable := sel.Provider.(provider.TokenCounter)
	var entry *results.EvalEntry
	if file != nil {
		entry = file.Evals[sel.Key()]
	}
	return evalSkipReason(entry, app, sel.Model, execute, reportsUsage, countCapable, alwaysRerunEval) == ""
}
