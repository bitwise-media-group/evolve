// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/results"
)

// SelectReason is why a single case (one trigger/eval, for one model) is
// preselected by a --new/--failed/--modified sweep. ReasonNone means the stored
// result is complete and unchanged and the case is not selected. The same value
// decides both what the engine reruns and what the TUI form annotates, so the
// two never diverge: a (case, model) runs exactly when its reason is not
// ReasonNone.
type SelectReason int

const (
	ReasonNone                SelectReason = iota // complete; not selected
	ReasonNew                                     // no stored result for this model
	ReasonModified                                // authored skill content or case definition changed (--modified)
	ReasonNotPassing                              // a complete result graded as failing
	ReasonErrored                                 // an eval whose agent run errored
	ReasonIncompleteRun                           // missing pass/timing fields a rerun fills
	ReasonMissingInputTokens                      // measured input usage absent
	ReasonMissingOutputTokens                     // measured output usage absent
	ReasonMissingMeasuredCost                     // measured cost absent
	ReasonBaselineMissing                         // without-skill baseline missing or stale (--baseline)
	ReasonNoData                                  // aggregate: no data across every selected model
)

// String is the grey annotation shown beside a preselected case.
func (r SelectReason) String() string {
	switch r {
	case ReasonNew:
		return "new"
	case ReasonModified:
		return "modified"
	case ReasonNotPassing:
		return "not passing (failed)"
	case ReasonErrored:
		return "errored"
	case ReasonIncompleteRun:
		return "incomplete run"
	case ReasonMissingInputTokens:
		return "missing input tokens"
	case ReasonMissingOutputTokens:
		return "missing output tokens"
	case ReasonMissingMeasuredCost:
		return "missing measured costs"
	case ReasonBaselineMissing:
		return "needs baseline"
	case ReasonNoData:
		return "no data for selected models"
	default:
		return ""
	}
}

// fingerprints carries the hashes the case-reason functions compare for
// --modified: a case's freshly computed content and spec hashes against the
// content hash stored on its results entry. (The stored spec hash is read off
// the result itself.) A case is modified when its stored spec hash exists and
// differs, or when both content hashes exist and differ. An empty stored hash
// means no baseline, so a pre-fingerprinting result is never spuriously flagged.
type fingerprints struct {
	storedContent string // content hash from the stored entry's Header
	freshContent  string // content hash recomputed from the current skill
	freshSpec     string // spec hash recomputed from the current case JSON
}

// modified reports whether stored and fresh fingerprints diverge, gated on a
// stored baseline (storedSpec non-empty) so empty pre-feature hashes never match.
func (fp fingerprints) modified(storedSpec string) bool {
	if storedSpec == "" {
		return false // no baseline recorded for this case
	}
	if storedSpec != fp.freshSpec {
		return true
	}
	return fp.storedContent != "" && fp.storedContent != fp.freshContent
}

// triggerCaseReason classifies why one trigger query is preselected for one
// model, or ReasonNone when its stored result is complete and unchanged.
// Token-count estimates are intentionally not a reason: the TUI cannot probe
// them cheaply, so keeping them out keeps CLI and TUI selection identical (the
// count still refreshes whenever the case runs for another reason).
func triggerCaseReason(r results.TriggerResult, ok bool,
	execute, wantNew, wantFailed, wantModified bool, fp fingerprints,
) SelectReason {
	if wantFailed && execute && ok && r.Passed != nil && !*r.Passed {
		return ReasonNotPassing
	}
	if wantModified && ok && fp.modified(r.SpecHash) {
		return ReasonModified
	}
	if wantNew {
		if !ok {
			return ReasonNew
		}
		if execute && (r.Hits == nil || r.Runs == nil || r.Passed == nil || r.AvgRunSeconds == nil) {
			return ReasonIncompleteRun
		}
	}
	return ReasonNone
}

// evalCaseReason classifies why one eval is preselected for one model, or
// ReasonNone when complete and unchanged. Like triggerCaseReason it ignores
// token-count estimates; the measured-usage reasons cover the fields a real run
// produces.
func evalCaseReason(r results.EvalResult, ok bool,
	execute, reportsUsage, priced, wantNew, wantFailed, wantModified bool, fp fingerprints,
) SelectReason {
	if wantFailed && execute && ok {
		if r.RuntimeError != "" {
			return ReasonErrored
		}
		if r.Passed != nil && !*r.Passed {
			return ReasonNotPassing
		}
	}
	if wantModified && ok && fp.modified(r.SpecHash) {
		return ReasonModified
	}
	if wantNew {
		if !ok {
			return ReasonNew
		}
		if execute {
			if r.RuntimeError != "" {
				return ReasonErrored
			}
			if r.Passed == nil || r.Timing == nil || r.Timing.ExecutorDurationSeconds == nil {
				return ReasonIncompleteRun
			}
			if reportsUsage {
				if r.Measured == nil || r.Measured.InputTokens == nil {
					return ReasonMissingInputTokens
				}
				if r.Measured.OutputTokens == nil {
					return ReasonMissingOutputTokens
				}
				if priced && r.Measured.CostUSD == nil {
					return ReasonMissingMeasuredCost
				}
			}
		}
	}
	return ReasonNone
}

// aggregateReasons collapses one case's per-model reasons (only the applicable
// models — those that do not skip the case) into the single note shown beside
// it: "" when no model needs it, "no data for selected models" when every
// applicable model lacks data, otherwise the distinct reasons joined in order.
func aggregateReasons(perModel []SelectReason) string {
	var distinct []SelectReason
	seen := map[SelectReason]bool{}
	any, allNew := false, true
	for _, r := range perModel {
		if r == ReasonNone {
			allNew = false
			continue
		}
		any = true
		if r != ReasonNew {
			allNew = false
		}
		if !seen[r] {
			seen[r] = true
			distinct = append(distinct, r)
		}
	}
	if !any {
		return ""
	}
	if allNew {
		return ReasonNoData.String()
	}
	parts := make([]string, len(distinct))
	for i, r := range distinct {
		parts[i] = r.String()
	}
	return strings.Join(parts, ", ")
}

// lookupTrigger finds a stored trigger result by query within one model's entry,
// returning the entry's content hash alongside it for --modified comparison.
func lookupTrigger(file *results.File, key, query string) (r results.TriggerResult, contentHash string, ok bool) {
	if file == nil {
		return results.TriggerResult{}, "", false
	}
	entry := file.Trigger(key)
	if entry == nil {
		return results.TriggerResult{}, "", false
	}
	for _, res := range entry.Results {
		if res.Query == query {
			return res, entry.ContentHash, true
		}
	}
	return results.TriggerResult{}, "", false
}

// lookupEval finds a stored eval result by id within one model's entry,
// returning the entry's content hash alongside it for --modified comparison.
func lookupEval(file *results.File, key, id string) (r results.EvalResult, contentHash string, ok bool) {
	if file == nil {
		return results.EvalResult{}, "", false
	}
	entry := file.Eval(key)
	if entry == nil {
		return results.EvalResult{}, "", false
	}
	for _, res := range entry.Results {
		if res.ID == id {
			return res, entry.ContentHash, true
		}
	}
	return results.EvalResult{}, "", false
}

// StaleTiers reports whether set's stored triggers/evals evidence in file is
// stale against the currently authored content — the same "would --modified
// rerun this" comparison the sweep engine uses (fingerprints.modified), reused
// here rather than reimplemented. A tier with no stored entries, or whose
// authored file is absent, is never stale (nothing to compare). Staleness is
// per skill, not per case: any model whose stored content hash or any result's
// stored spec hash diverges from the fresh value marks the tier stale.
func StaleTiers(set layout.EvalSet, file *results.File) (triggersStale, evalsStale bool) {
	if file == nil {
		return false, false
	}
	if set.TriggersPath != "" {
		if tf, err := evalspec.LoadTriggers(set.TriggersPath); err == nil {
			var content string
			if md, err := os.ReadFile(filepath.Join(set.SkillDir, "SKILL.md")); err == nil {
				content = triggerContentHash(md)
			}
			triggersStale = staleTriggers(file, tf.Triggers, content)
		}
	}
	if set.EvalsPath != "" {
		if ef, err := evalspec.LoadEvals(set.EvalsPath); err == nil {
			content, _ := skillContentHash(set.SkillDir)
			evalsStale = staleEvals(file, ef.Evals, content)
		}
	}
	return triggersStale, evalsStale
}

// staleTriggers reports whether any model's stored trigger entry in file
// diverges from the freshly authored triggers, by content hash (the entry's
// Header.ContentHash) or per-query spec hash.
func staleTriggers(file *results.File, triggers []evalspec.Trigger, freshContent string) bool {
	specs := make(map[string]string, len(triggers))
	for _, t := range triggers {
		specs[t.Query] = specHash(t)
	}
	for _, key := range file.ModelKeys() {
		entry := file.Trigger(key)
		if entry == nil {
			continue
		}
		fp := fingerprints{storedContent: entry.ContentHash, freshContent: freshContent}
		for _, r := range entry.Results {
			fp.freshSpec = specs[r.Query]
			if fp.modified(r.SpecHash) {
				return true
			}
		}
	}
	return false
}

// staleEvals is staleTriggers for the eval tier, comparing each result's
// stored spec hash against the fresh evalFingerprint (spec + fixtures).
func staleEvals(file *results.File, evals []evalspec.Eval, freshContent string) bool {
	specs := make(map[string]string, len(evals))
	for _, e := range evals {
		specs[e.ID] = evalFingerprint(e)
	}
	for _, key := range file.ModelKeys() {
		entry := file.Eval(key)
		if entry == nil {
			continue
		}
		fp := fingerprints{storedContent: entry.ContentHash, freshContent: freshContent}
		for _, r := range entry.Results {
			fp.freshSpec = specs[r.ID]
			if fp.modified(r.SpecHash) {
				return true
			}
		}
	}
	return false
}
