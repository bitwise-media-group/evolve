// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/results"
)

// completeTrigger is a fully populated, passing trigger result.
func completeTrigger(shouldTrigger bool) results.TriggerResult {
	return results.TriggerResult{
		Query: "q", ShouldTrigger: shouldTrigger,
		Hits: new(3), Runs: new(3), Passed: new(true), AvgRunSeconds: new(1.0),
	}
}

func TestTriggerCaseReason(t *testing.T) {
	cases := []struct {
		name                           string
		r                              results.TriggerResult
		ok, wantNew, wantFail, wantMod bool
		fp                             fingerprints
		want                           SelectReason
	}{
		{"complete passing", completeTrigger(true), true, true, true, false, fingerprints{}, ReasonNone},
		{"missing under new", results.TriggerResult{}, false, true, false, false, fingerprints{}, ReasonNew},
		{"missing under failed-only", results.TriggerResult{}, false, false, true, false, fingerprints{}, ReasonNone},
		// A should_trigger flip is no longer caught by --new; it is a --modified
		// concern (the spec hash covers it), so under --new alone it is ReasonNone.
		{"should_trigger change not caught by --new", completeTrigger(false), true, true, false, false, fingerprints{}, ReasonNone},
		{"incomplete run", results.TriggerResult{Query: "q", ShouldTrigger: true}, true, true, false, false, fingerprints{}, ReasonIncompleteRun},
		{"failed under failed", completeFailingTrigger(), true, false, true, false, fingerprints{}, ReasonNotPassing},
		{"failed ignored under new-only", completeFailingTrigger(), true, true, false, false, fingerprints{}, ReasonNone},
		{"spec changed under modified", triggerWithSpec("old"), true, false, false, true, fingerprints{freshSpec: "new"}, ReasonModified},
		{"content changed under modified", triggerWithSpec("s"), true, false, false, true, fingerprints{storedContent: "old", freshContent: "new", freshSpec: "s"}, ReasonModified},
		{"unchanged under modified", triggerWithSpec("s"), true, false, false, true, fingerprints{storedContent: "c", freshContent: "c", freshSpec: "s"}, ReasonNone},
		{"no baseline under modified", completeTrigger(true), true, false, false, true, fingerprints{freshSpec: "new"}, ReasonNone},
		{"modified ignored when flag off", triggerWithSpec("old"), true, true, false, false, fingerprints{freshSpec: "new"}, ReasonNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := triggerCaseReason(c.r, c.ok, true /* execute */, c.wantNew, c.wantFail, c.wantMod, c.fp)
			if got != c.want {
				t.Errorf("got %v (%q), want %v", got, got, c.want)
			}
		})
	}
}

func completeFailingTrigger() results.TriggerResult {
	r := completeTrigger(true)
	r.Passed = new(false)
	return r
}

// triggerWithSpec is a complete, passing trigger result carrying a stored spec
// hash, for exercising --modified detection.
func triggerWithSpec(specHash string) results.TriggerResult {
	r := completeTrigger(true)
	r.SpecHash = specHash
	return r
}

func completeEval(passed bool) results.EvalResult {
	return results.EvalResult{
		ID: "e", Passed: new(passed),
		Timing:   &results.Timing{ExecutorDurationSeconds: new(2.0)},
		Measured: &results.Measured{InputTokens: new(100), OutputTokens: new(10), CostUSD: new(0.1)},
	}
}

func TestEvalCaseReason(t *testing.T) {
	cases := []struct {
		name                       string
		r                          results.EvalResult
		ok, reportsUsage, priced   bool
		wantNew, wantFail, wantMod bool
		fp                         fingerprints
		want                       SelectReason
	}{
		{"complete passing", completeEval(true), true, true, true, true, true, false, fingerprints{}, ReasonNone},
		{"missing under new", results.EvalResult{}, false, true, true, true, false, false, fingerprints{}, ReasonNew},
		{"missing under failed-only", results.EvalResult{}, false, true, true, false, true, false, fingerprints{}, ReasonNone},
		{"runtime error under failed", results.EvalResult{ID: "e", RuntimeError: "boom"}, true, true, true, false, true, false, fingerprints{}, ReasonErrored},
		{"failed assertions under failed", completeEval(false), true, true, true, false, true, false, fingerprints{}, ReasonNotPassing},
		{"incomplete (no timing) under new", results.EvalResult{ID: "e", Passed: new(true)}, true, true, true, true, false, false, fingerprints{}, ReasonIncompleteRun},
		{"missing input tokens", evalMissingMeasured(nil), true, true, true, true, false, false, fingerprints{}, ReasonMissingInputTokens},
		{"missing output tokens", evalMissingMeasured(&results.Measured{InputTokens: new(100)}), true, true, true, true, false, false, fingerprints{}, ReasonMissingOutputTokens},
		{"missing measured cost", evalMissingMeasured(&results.Measured{InputTokens: new(100), OutputTokens: new(10)}), true, true, true, true, false, false, fingerprints{}, ReasonMissingMeasuredCost},
		{"usage ignored when provider does not report", evalMissingMeasured(nil), true, false, false, true, false, false, fingerprints{}, ReasonNone},
		{"spec changed under modified", evalWithSpec("old"), true, true, true, false, false, true, fingerprints{freshSpec: "new"}, ReasonModified},
		{"content changed under modified", evalWithSpec("s"), true, true, true, false, false, true, fingerprints{storedContent: "old", freshContent: "new", freshSpec: "s"}, ReasonModified},
		{"unchanged under modified", evalWithSpec("s"), true, true, true, false, false, true, fingerprints{storedContent: "c", freshContent: "c", freshSpec: "s"}, ReasonNone},
		{"no baseline under modified", completeEval(true), true, true, true, false, false, true, fingerprints{freshSpec: "new"}, ReasonNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := evalCaseReason(c.r, c.ok, true /* execute */, c.reportsUsage, c.priced, c.wantNew, c.wantFail, c.wantMod, c.fp)
			if got != c.want {
				t.Errorf("got %v (%q), want %v", got, got, c.want)
			}
		})
	}
}

// evalWithSpec is a complete, passing eval result carrying a stored spec hash,
// for exercising --modified detection.
func evalWithSpec(specHash string) results.EvalResult {
	r := completeEval(true)
	r.SpecHash = specHash
	return r
}

// evalMissingMeasured is a graded, timed eval result with the given (partial or
// nil) measured usage — used to exercise the per-field missing-usage reasons.
func evalMissingMeasured(m *results.Measured) results.EvalResult {
	return results.EvalResult{
		ID: "e", Passed: new(true),
		Timing:   &results.Timing{ExecutorDurationSeconds: new(2.0)},
		Measured: m,
	}
}

// staleSet writes a single-plugin repo with one skill authoring one trigger
// query and one eval id, and returns its detected EvalSet. Callers pair it with
// a results.File carrying stored hashes to exercise StaleTiers across both tiers.
func staleSet(t *testing.T) layout.EvalSet {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude-plugin/plugin.json", `{"name":"solo","version":"0.1.0"}`)
	write("skills/solo-skill/SKILL.md", "---\nname: solo-skill\n---\nbody\n")
	write("evals/solo-skill/triggers.json", `{"triggers":[{"query":"q","should_trigger":true}]}`)
	write("evals/solo-skill/evals.json",
		`{"evals":[{"id":"basic","prompt":"p","assertions":[{"type":"file_exists","path":"x"}]}]}`)

	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	sets, err := repo.EvalSets()
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 1 {
		t.Fatalf("got %d eval sets, want 1", len(sets))
	}
	return sets[0]
}

// TestStaleTiers pins the report gate's staleness primitive across both tiers.
// The triggers path was entirely untested; this covers the fresh (not stale),
// diverged-hash (stale), and removed-case (stored case absent from the authored
// file → stale via empty fresh spec) scenarios for triggers and evals alike.
func TestStaleTiers(t *testing.T) {
	set := staleSet(t)

	// Freshly authored hashes, computed the same way StaleTiers recomputes them.
	skillMD, err := os.ReadFile(filepath.Join(set.SkillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	freshTriggerContent := triggerContentHash(skillMD)
	freshTriggerSpec := specHash(evalspec.Trigger{Query: "q", ShouldTrigger: true})
	freshEvalContent, err := skillContentHash(set.SkillDir)
	if err != nil {
		t.Fatal(err)
	}
	tf, err := evalspec.LoadEvals(set.EvalsPath)
	if err != nil {
		t.Fatal(err)
	}
	freshEvalSpec := evalFingerprint(tf.Evals[0])

	// fileWith builds a results.File whose one trigger and one eval carry the
	// given stored query/id, content hash, and spec hash.
	fileWith := func(query, triggerContent, triggerSpec, evalID, evalContent, evalSpec string) *results.File {
		f := &results.File{Schema: results.Schema, Plugin: "solo", Skill: "solo-skill"}
		f.SetTrigger("anthropic/claude-fable-5", &results.TriggerEntry{
			Header:  results.Header{Provider: "anthropic", Model: "claude-fable-5", ContentHash: triggerContent},
			Results: []results.TriggerResult{{Query: query, SpecHash: triggerSpec}},
		})
		f.SetEval("anthropic/claude-fable-5", &results.EvalEntry{
			Header:  results.Header{Provider: "anthropic", Model: "claude-fable-5", ContentHash: evalContent},
			Results: []results.EvalResult{{ID: evalID, SpecHash: evalSpec}},
		})
		return f
	}

	t.Run("fresh evidence is not stale", func(t *testing.T) {
		file := fileWith("q", freshTriggerContent, freshTriggerSpec, "basic", freshEvalContent, freshEvalSpec)
		if ts, es := StaleTiers(set, file); ts || es {
			t.Errorf("triggersStale=%v evalsStale=%v, want both false", ts, es)
		}
	})

	t.Run("diverged spec hash stales both tiers", func(t *testing.T) {
		file := fileWith("q", freshTriggerContent, "does-not-match", "basic", freshEvalContent, "does-not-match")
		ts, es := StaleTiers(set, file)
		if !ts || !es {
			t.Errorf("triggersStale=%v evalsStale=%v, want both true", ts, es)
		}
	})

	t.Run("removed case stales via empty fresh spec", func(t *testing.T) {
		// The stored query/id are absent from the authored files, so their fresh
		// spec hash is "". A non-empty stored spec against an empty fresh one is
		// modified — pinning fingerprints.modified's empty-hash semantics: a
		// removed case must read as stale, not silently equal.
		file := fileWith("gone", freshTriggerContent, "stored-hash", "removed", freshEvalContent, "stored-hash")
		ts, es := StaleTiers(set, file)
		if !ts || !es {
			t.Errorf("triggersStale=%v evalsStale=%v, want both true (removed case)", ts, es)
		}
	})

	t.Run("nil file is never stale", func(t *testing.T) {
		if ts, es := StaleTiers(set, nil); ts || es {
			t.Errorf("triggersStale=%v evalsStale=%v, want both false for nil file", ts, es)
		}
	})
}

func TestAggregateReasons(t *testing.T) {
	cases := []struct {
		name string
		in   []SelectReason
		want string
	}{
		{"none", nil, ""},
		{"all complete", []SelectReason{ReasonNone, ReasonNone}, ""},
		{"all new", []SelectReason{ReasonNew, ReasonNew}, "no data for selected models"},
		{"some new some complete", []SelectReason{ReasonNew, ReasonNone}, "new"},
		{"mixed reasons", []SelectReason{ReasonNotPassing, ReasonMissingOutputTokens}, "not passing (failed), missing output tokens"},
		{"deduped", []SelectReason{ReasonNotPassing, ReasonNotPassing}, "not passing (failed)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := aggregateReasons(c.in); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
