// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"testing"

	"github.com/bitwise-media-group/evolve/internal/results"
)

func bptr(b bool) *bool         { return &b }
func iptr(n int) *int           { return &n }
func f64ptr(f float64) *float64 { return &f }

// completeTrigger is a fully populated, passing trigger result.
func completeTrigger(shouldTrigger bool) results.TriggerResult {
	return results.TriggerResult{
		Query: "q", ShouldTrigger: shouldTrigger,
		Hits: iptr(3), Runs: iptr(3), Passed: bptr(true), AvgRunSeconds: f64ptr(1.0),
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
	r.Passed = bptr(false)
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
		ID: "e", Passed: bptr(passed),
		Timing:   &results.Timing{ExecutorDurationSeconds: f64ptr(2.0)},
		Measured: &results.Measured{InputTokens: iptr(100), OutputTokens: iptr(10), CostUSD: f64ptr(0.1)},
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
		{"incomplete (no timing) under new", results.EvalResult{ID: "e", Passed: bptr(true)}, true, true, true, true, false, false, fingerprints{}, ReasonIncompleteRun},
		{"missing input tokens", evalMissingMeasured(nil), true, true, true, true, false, false, fingerprints{}, ReasonMissingInputTokens},
		{"missing output tokens", evalMissingMeasured(&results.Measured{InputTokens: iptr(100)}), true, true, true, true, false, false, fingerprints{}, ReasonMissingOutputTokens},
		{"missing measured cost", evalMissingMeasured(&results.Measured{InputTokens: iptr(100), OutputTokens: iptr(10)}), true, true, true, true, false, false, fingerprints{}, ReasonMissingMeasuredCost},
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
		ID: "e", Passed: bptr(true),
		Timing:   &results.Timing{ExecutorDurationSeconds: f64ptr(2.0)},
		Measured: m,
	}
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
