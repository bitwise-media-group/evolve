// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package plan

import "github.com/bitwise-media-group/evolve/internal/results"

// PriorMetrics holds the last committed per-case metrics a live run is compared
// against: the current results (the basis for vs-previous) and any without-skill
// baseline (the basis for vs-baseline, evals only). The dashboard seeds it once
// before a run so it can color deltas as cases finish — comparing the live run to
// the run it is about to replace, exactly as the committed report does.
type PriorMetrics struct {
	eval     map[priorKey]results.EvalResult
	baseline map[priorKey]results.EvalResult
	trigger  map[priorKey]results.TriggerResult
}

type priorKey struct {
	ref   UnitRef
	label string
}

// LoadPriorMetrics reads each skill's committed results and indexes the current
// eval/trigger results and the eval baseline, keyed by unit and case label.
// Missing or unreadable results simply contribute nothing.
func LoadPriorMetrics(cat []SkillCatalog) PriorMetrics {
	pm := PriorMetrics{
		eval:     map[priorKey]results.EvalResult{},
		baseline: map[priorKey]results.EvalResult{},
		trigger:  map[priorKey]results.TriggerResult{},
	}
	for _, sc := range cat {
		file, _, _ := results.LoadDir(sc.ResultsDir, sc.Plugin, sc.Skill)
		for key, m := range file.Models {
			if m.Evals != nil {
				ref := UnitRef{Skill: sc.Skill, Key: key, Kind: KindEvals}
				for _, r := range m.Evals.Results {
					pm.eval[priorKey{ref, r.ID}] = r
				}
				if m.Evals.Baseline != nil {
					for _, r := range m.Evals.Baseline.Results {
						pm.baseline[priorKey{ref, r.ID}] = r
					}
				}
			}
			if m.Triggers != nil {
				ref := UnitRef{Skill: sc.Skill, Key: key, Kind: KindTriggers}
				for _, r := range m.Triggers.Results {
					pm.trigger[priorKey{ref, r.Query}] = r
				}
			}
		}
	}
	return pm
}

// EvalPrevious returns the prior committed eval result for (ref, label).
func (pm PriorMetrics) EvalPrevious(ref UnitRef, label string) (results.EvalResult, bool) {
	m, ok := pm.eval[priorKey{ref, label}]
	return m, ok
}

// EvalBaseline returns the committed without-skill baseline result for (ref, label).
func (pm PriorMetrics) EvalBaseline(ref UnitRef, label string) (results.EvalResult, bool) {
	m, ok := pm.baseline[priorKey{ref, label}]
	return m, ok
}

// TriggerPrevious returns the prior committed trigger result for (ref, label).
func (pm PriorMetrics) TriggerPrevious(ref UnitRef, label string) (results.TriggerResult, bool) {
	m, ok := pm.trigger[priorKey{ref, label}]
	return m, ok
}
