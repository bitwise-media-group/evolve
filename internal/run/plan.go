// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"os"
	"path/filepath"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/manifest"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/results"
)

// Catalog loads every skill's triggers, evals, and SKILL.md metadata across the
// repository. It ignores the plugin/skill/eval filters so the form can show the full
// tree and merely preselect the flag-narrowed subset. A skill whose spec fails
// to parse is included with whatever loaded (so the UI still lists it).
func Catalog(opts Options) ([]plan.SkillCatalog, error) {
	sets, err := opts.Repo.EvalSets()
	if err != nil {
		return nil, err
	}
	cat := make([]plan.SkillCatalog, 0, len(sets))
	for _, set := range sets {
		sc := plan.SkillCatalog{Plugin: set.Plugin.Name, Skill: set.Skill, SkillDir: set.SkillDir, ResultsDir: set.ResultsDir}
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
// with at least one applicable case. It reuses the planner's applicability
// checks so the planned list cannot drift from what the engine runs.
func Plan(cat []plan.SkillCatalog, sels []provider.Selection, f *plan.Filter, tiers plan.Tiers) []plan.UnitRef {
	var units []plan.UnitRef
	for _, sc := range cat {
		for _, sel := range sels {
			if tiers.Triggers && len(plan.ApplicableTriggers(sc.Triggers, sel.Provider.Name(), sc.Skill, f)) > 0 {
				units = append(units, plan.UnitRef{Skill: sc.Skill, Key: sel.Key(), Kind: plan.KindTriggers})
			}
			if tiers.Evals && len(plan.ApplicableEvals(sc.Evals, sel.Provider.Name(), sc.Skill, f)) > 0 {
				units = append(units, plan.UnitRef{Skill: sc.Skill, Key: sel.Key(), Kind: plan.KindEvals})
			}
		}
	}
	return units
}

// PlanFor enumerates the units one selection would run under a per-model filter.
func PlanFor(cat []plan.SkillCatalog, sel provider.Selection, f *plan.Filter, tiers plan.Tiers) []plan.UnitRef {
	return Plan(cat, []provider.Selection{sel}, f, tiers)
}

// Needs reports, per resolved selection (keyed by Selection.Key()) and per
// applicable case, whether the engine would run that case, plus a per-case note
// explaining why it is preselected. Without --new/--failed every applicable case
// runs (and notes is empty); with them, a case runs exactly when its
// SelectReason is not ReasonNone — the same predicate the engine uses — so the
// form's initial selection matches non-TUI mode case for case. Only cases under
// def's tiers, the plugin/skill filters, and evalFilter, and applicable for the model (its
// skip_providers honored), appear. Token-count estimates are deliberately not a
// reason here nor in the engine, so this needs no token-counting round trip.
func Needs(
	opts Options, cat []plan.SkillCatalog, sels []provider.Selection, def plan.Tiers, evalFilter string,
) (need map[string]map[plan.CaseRef]bool, notes map[plan.CaseRef]string) {
	need = make(map[string]map[plan.CaseRef]bool, len(sels))
	for _, sel := range sels {
		need[sel.Key()] = map[plan.CaseRef]bool{}
	}
	notes = map[plan.CaseRef]string{}
	flags := opts.New || opts.Failed || opts.Modified
	for _, sc := range cat {
		if !opts.selects(sc.Plugin, sc.Skill) {
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
func needContentHashes(opts Options, sc plan.SkillCatalog, def plan.Tiers) (triggerContent, evalContent string) {
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
func needTriggers(opts Options, sc plan.SkillCatalog, sels []provider.Selection, flags bool,
	file *results.File, content string, need map[string]map[plan.CaseRef]bool, notes map[plan.CaseRef]string) {

	for _, t := range sc.Triggers {
		cr := plan.CaseRef{Skill: sc.Skill, Kind: plan.KindTriggers, Case: t.Query}
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
func needEvals(opts Options, sc plan.SkillCatalog, sels []provider.Selection, flags bool,
	file *results.File, content, evalFilter string, need map[string]map[plan.CaseRef]bool, notes map[plan.CaseRef]string) {

	for _, c := range sc.Evals {
		if evalFilter != "" && c.ID != evalFilter {
			continue
		}
		cr := plan.CaseRef{Skill: sc.Skill, Kind: plan.KindEvals, Case: c.ID}
		var freshSpec string
		if opts.Modified {
			freshSpec = evalFingerprint(c)
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
				// A missing/stale baseline is an additive gap (--baseline): it selects
				// the eval even when --new/--failed/--modified would not, matching the
				// engine's run-set.
				if reason == ReasonNone && evalBaselineNeeded(file, sel.Key(), c, execute, opts.Baseline) {
					reason = ReasonBaselineMissing
				}
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
