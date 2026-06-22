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
	eval     map[priorKey]results.EvalCaseMetrics
	baseline map[priorKey]results.EvalCaseMetrics
	trigger  map[priorKey]results.TriggerCaseMetrics
}

type priorKey struct {
	ref   UnitRef
	label string
}

// LoadPriorMetrics reads each skill's committed results and indexes the current
// eval/trigger case metrics and the eval baseline, keyed by unit and case label.
// Missing or unreadable results simply contribute nothing.
func LoadPriorMetrics(cat []SkillCatalog) PriorMetrics {
	pm := PriorMetrics{
		eval:     map[priorKey]results.EvalCaseMetrics{},
		baseline: map[priorKey]results.EvalCaseMetrics{},
		trigger:  map[priorKey]results.TriggerCaseMetrics{},
	}
	for _, sc := range cat {
		file, _ := results.LoadDir(sc.ResultsDir, sc.Plugin, sc.Skill)
		for key, entry := range file.Evals {
			ref := UnitRef{Skill: sc.Skill, Key: key, Kind: KindEvals}
			for _, r := range entry.Results {
				pm.eval[priorKey{ref, r.ID}] = results.EvalCaseMetricsOf(r)
			}
			if entry.Baseline != nil {
				for id, m := range entry.Baseline.Cases {
					pm.baseline[priorKey{ref, id}] = m
				}
			}
		}
		for key, entry := range file.Triggers {
			ref := UnitRef{Skill: sc.Skill, Key: key, Kind: KindTriggers}
			for _, r := range entry.Results {
				pm.trigger[priorKey{ref, r.Query}] = results.TriggerCaseMetricsOf(r)
			}
		}
	}
	return pm
}

// EvalPrevious returns the prior committed eval case metrics for (ref, label).
func (pm PriorMetrics) EvalPrevious(ref UnitRef, label string) (results.EvalCaseMetrics, bool) {
	m, ok := pm.eval[priorKey{ref, label}]
	return m, ok
}

// EvalBaseline returns the committed without-skill baseline metrics for (ref, label).
func (pm PriorMetrics) EvalBaseline(ref UnitRef, label string) (results.EvalCaseMetrics, bool) {
	m, ok := pm.baseline[priorKey{ref, label}]
	return m, ok
}

// TriggerPrevious returns the prior committed trigger case metrics for (ref, label).
func (pm PriorMetrics) TriggerPrevious(ref UnitRef, label string) (results.TriggerCaseMetrics, bool) {
	m, ok := pm.trigger[priorKey{ref, label}]
	return m, ok
}
