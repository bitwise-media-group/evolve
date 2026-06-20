// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"os"
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
	SkillDir    string // the skill's root directory, fingerprinted for --modified
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
		sc := SkillCatalog{Plugin: set.Plugin.Name, Skill: set.Skill, SkillDir: set.SkillDir, ResultsDir: set.ResultsDir}
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

// CaseRef identifies one authored case (a trigger query or eval id) within a
// tier, independent of any model. It is the key the selection form and the
// per-case run matrix share.
type CaseRef struct {
	Skill string
	Kind  Kind
	Case  string // trigger query or eval id
}

// Needs reports, per resolved selection (keyed by Selection.Key()) and per
// applicable case, whether the engine would run that case, plus a per-case note
// explaining why it is preselected. Without --new/--failed every applicable case
// runs (and notes is empty); with them, a case runs exactly when its
// SelectReason is not ReasonNone — the same predicate the engine uses — so the
// form's initial selection matches non-TUI mode case for case. Only cases under
// def's tiers, SkillFilter, and evalFilter, and applicable for the model (its
// skip_providers honored), appear. Token-count estimates are deliberately not a
// reason here nor in the engine, so this needs no token-counting round trip.
func Needs(
	opts Options, cat []SkillCatalog, sels []provider.Selection, def Tiers, evalFilter string,
) (need map[string]map[CaseRef]bool, notes map[CaseRef]string) {
	need = make(map[string]map[CaseRef]bool, len(sels))
	for _, sel := range sels {
		need[sel.Key()] = map[CaseRef]bool{}
	}
	notes = map[CaseRef]string{}
	flags := opts.New || opts.Failed || opts.Modified
	for _, sc := range cat {
		if opts.SkillFilter != "" && sc.Skill != opts.SkillFilter {
			continue
		}
		var file *results.File
		// Content fingerprints are needed only for --modified; computed once per
		// skill, identical across every model and case in the skill.
		var triggerContent, evalContent string
		if flags {
			file, _ = results.LoadDir(sc.ResultsDir, sc.Plugin, sc.Skill)
			triggerContent, evalContent = needContentHashes(opts, sc, def)
		}
		if def.Triggers {
			needTriggers(opts, sc, sels, flags, file, triggerContent, need, notes)
		}
		if def.Evals {
			needEvals(opts, sc, sels, flags, file, evalContent, evalFilter, need, notes)
		}
	}
	return need, notes
}

// needContentHashes computes a skill's per-tier content fingerprints for the
// --modified preview, or empty strings when --modified is off (the only flag
// that consults them). Empty on any read/walk error: a missing fingerprint is
// treated as "no baseline", never a spurious modification.
func needContentHashes(opts Options, sc SkillCatalog, def Tiers) (triggerContent, evalContent string) {
	if !opts.Modified {
		return "", ""
	}
	if def.Triggers {
		if md, err := os.ReadFile(filepath.Join(sc.SkillDir, "SKILL.md")); err == nil {
			triggerContent = triggerContentHash(md)
		}
	}
	if def.Evals {
		evalContent, _ = skillContentHash(sc.SkillDir)
	}
	return triggerContent, evalContent
}

// needTriggers records, for each of a skill's triggers, whether each model would
// run it and the aggregate preselect note — the same predicate the engine uses.
func needTriggers(opts Options, sc SkillCatalog, sels []provider.Selection, flags bool,
	file *results.File, content string, need map[string]map[CaseRef]bool, notes map[CaseRef]string) {

	for _, t := range sc.Triggers {
		cr := CaseRef{Skill: sc.Skill, Kind: KindTriggers, Case: t.Query}
		var freshSpec string
		if opts.Modified {
			freshSpec = specHash(t)
		}
		var perModel []SelectReason
		for _, sel := range sels {
			if t.SkipsProvider(sel.Provider.Name()) {
				continue
			}
			reason := ReasonNone
			if flags {
				r, storedContent, ok := lookupTrigger(file, sel.Key(), t.Query)
				fp := fingerprints{storedContent: storedContent, freshContent: content, freshSpec: freshSpec}
				reason = triggerCaseReason(r, ok, triggerExecutes(opts, sel), opts.New, opts.Failed, opts.Modified, fp)
			}
			perModel = append(perModel, reason)
			need[sel.Key()][cr] = !flags || reason != ReasonNone
		}
		if note := aggregateReasons(perModel); note != "" {
			notes[cr] = note
		}
	}
}

// needEvals is needTriggers for the eval tier, honoring evalFilter.
func needEvals(opts Options, sc SkillCatalog, sels []provider.Selection, flags bool,
	file *results.File, content, evalFilter string, need map[string]map[CaseRef]bool, notes map[CaseRef]string) {

	for _, c := range sc.Evals {
		if evalFilter != "" && c.ID != evalFilter {
			continue
		}
		cr := CaseRef{Skill: sc.Skill, Kind: KindEvals, Case: c.ID}
		var freshSpec string
		if opts.Modified {
			freshSpec = specHash(c)
		}
		var perModel []SelectReason
		for _, sel := range sels {
			if c.SkipsProvider(sel.Provider.Name()) {
				continue
			}
			reason := ReasonNone
			if flags {
				r, storedContent, ok := lookupEval(file, sel.Key(), c.ID)
				execute, reportsUsage, priced := evalCapabilities(opts, sel)
				fp := fingerprints{storedContent: storedContent, freshContent: content, freshSpec: freshSpec}
				reason = evalCaseReason(r, ok, execute, reportsUsage, priced, opts.New, opts.Failed, opts.Modified, fp)
			}
			perModel = append(perModel, reason)
			need[sel.Key()][cr] = !flags || reason != ReasonNone
		}
		if note := aggregateReasons(perModel); note != "" {
			notes[cr] = note
		}
	}
}

// triggerExecutes reports whether a trigger sweep would run agents for sel (vs
// token-count only): a CLI is on PATH and this is not a count-only invocation.
func triggerExecutes(opts Options, sel provider.Selection) bool {
	_, cliFound := provider.ResolveCLI(sel.Provider)
	return !opts.CountOnly && cliFound
}

// evalCapabilities mirrors runEvalUnit's per-model knobs: whether it executes,
// whether the provider reports measured usage, and whether the model is priced.
func evalCapabilities(opts Options, sel provider.Selection) (execute, reportsUsage, priced bool) {
	evalRunner, isEvalRunner := sel.Provider.(provider.EvalRunner)
	_, cliFound := provider.ResolveCLI(sel.Provider)
	execute = isEvalRunner && cliFound && !opts.CountOnly
	reportsUsage = isEvalRunner && evalRunner.ReportsUsage()
	priced = sel.Model.InputUSD != nil && sel.Model.OutputUSD != nil
	return execute, reportsUsage, priced
}
