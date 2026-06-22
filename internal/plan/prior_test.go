// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package plan

import (
	"path/filepath"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/results"
)

func TestLoadPriorMetrics(t *testing.T) {
	dir := t.TempDir()
	f := &results.File{Schema: results.Schema, Plugin: "p", Skill: "s"}
	f.SetEval("fake/m1", &results.EvalEntry{
		Header:  results.Header{Provider: "fake", Model: "m1", Executed: true},
		Results: []results.EvalResult{{ID: "e1", Passed: new(true), Summary: &results.GradeSummary{PassRate: new(1.0)}}},
		Summary: results.EvalSummary{Passed: new(1), Total: 1},
		Baseline: &results.EvalSnapshot{
			Summary: results.EvalSummary{Passed: new(0), Total: 1},
			Cases:   map[string]results.EvalCaseMetrics{"e1": {Passed: new(false), PassRate: new(0.0), Fingerprint: "fp"}},
		},
	})
	f.SetTrigger("fake/m1", &results.TriggerEntry{
		Header:  results.Header{Provider: "fake", Model: "m1", Executed: true},
		Results: []results.TriggerResult{{Query: "q1", Hits: new(2), Runs: new(3)}},
		Summary: results.TriggerSummary{Total: 1},
	})
	if _, err := f.SaveDir(dir, "json"); err != nil {
		t.Fatal(err)
	}

	cat := []SkillCatalog{{Plugin: "p", Skill: "s", ResultsDir: dir}}
	pm := LoadPriorMetrics(cat)

	evRef := UnitRef{Skill: "s", Key: "fake/m1", Kind: KindEvals}
	if m, ok := pm.EvalPrevious(evRef, "e1"); !ok || m.PassRate == nil || *m.PassRate != 1.0 {
		t.Errorf("eval previous = %+v ok=%v, want pass rate 1.0", m, ok)
	}
	if b, ok := pm.EvalBaseline(evRef, "e1"); !ok || b.Passed == nil || *b.Passed {
		t.Errorf("eval baseline = %+v ok=%v, want a failing baseline", b, ok)
	}
	trRef := UnitRef{Skill: "s", Key: "fake/m1", Kind: KindTriggers}
	if m, ok := pm.TriggerPrevious(trRef, "q1"); !ok || m.Hits == nil || *m.Hits != 2 {
		t.Errorf("trigger previous = %+v ok=%v, want hits 2", m, ok)
	}

	// Misses are clean, including the zero value (no committed results loaded).
	if _, ok := pm.EvalPrevious(evRef, "nope"); ok {
		t.Error("unknown eval label should miss")
	}
	if _, ok := (PriorMetrics{}).TriggerPrevious(trRef, "q1"); ok {
		t.Error("zero-value PriorMetrics must miss safely")
	}
}

func TestLoadPriorMetricsMissingDir(t *testing.T) {
	pm := LoadPriorMetrics([]SkillCatalog{{Plugin: "p", Skill: "s", ResultsDir: filepath.Join(t.TempDir(), "absent")}})
	if _, ok := pm.EvalPrevious(UnitRef{Skill: "s", Key: "fake/m1", Kind: KindEvals}, "e1"); ok {
		t.Error("missing results dir should contribute nothing")
	}
}
