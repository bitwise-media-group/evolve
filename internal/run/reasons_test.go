// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"testing"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
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
	spec := evalspec.Trigger{Query: "q", ShouldTrigger: true}

	cases := []struct {
		name                  string
		r                     results.TriggerResult
		ok, wantNew, wantFail bool
		want                  SelectReason
	}{
		{"complete passing", completeTrigger(true), true, true, true, ReasonNone},
		{"missing under new", results.TriggerResult{}, false, true, false, ReasonNew},
		{"missing under failed-only", results.TriggerResult{}, false, false, true, ReasonNone},
		{"should-trigger changed", completeTrigger(false), true, true, false, ReasonModified},
		{"incomplete run", results.TriggerResult{Query: "q", ShouldTrigger: true}, true, true, false, ReasonIncompleteRun},
		{"failed under failed", completeFailingTrigger(), true, false, true, ReasonNotPassing},
		{"failed ignored under new-only", completeFailingTrigger(), true, true, false, ReasonNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := triggerCaseReason(c.r, c.ok, spec, true /* execute */, c.wantNew, c.wantFail)
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

func completeEval(passed bool) results.EvalResult {
	return results.EvalResult{
		ID: "e", Passed: bptr(passed),
		Timing:   &results.Timing{ExecutorDurationSeconds: f64ptr(2.0)},
		Measured: &results.Measured{InputTokens: iptr(100), OutputTokens: iptr(10), CostUSD: f64ptr(0.1)},
	}
}

func TestEvalCaseReason(t *testing.T) {
	cases := []struct {
		name                     string
		r                        results.EvalResult
		ok, reportsUsage, priced bool
		wantNew, wantFail        bool
		want                     SelectReason
	}{
		{"complete passing", completeEval(true), true, true, true, true, true, ReasonNone},
		{"missing under new", results.EvalResult{}, false, true, true, true, false, ReasonNew},
		{"missing under failed-only", results.EvalResult{}, false, true, true, false, true, ReasonNone},
		{"runtime error under failed", results.EvalResult{ID: "e", RuntimeError: "boom"}, true, true, true, false, true, ReasonErrored},
		{"failed assertions under failed", completeEval(false), true, true, true, false, true, ReasonNotPassing},
		{"incomplete (no timing) under new", results.EvalResult{ID: "e", Passed: bptr(true)}, true, true, true, true, false, ReasonIncompleteRun},
		{"missing input tokens", evalMissingMeasured(nil), true, true, true, true, false, ReasonMissingInputTokens},
		{"missing output tokens", evalMissingMeasured(&results.Measured{InputTokens: iptr(100)}), true, true, true, true, false, ReasonMissingOutputTokens},
		{"missing measured cost", evalMissingMeasured(&results.Measured{InputTokens: iptr(100), OutputTokens: iptr(10)}), true, true, true, true, false, ReasonMissingMeasuredCost},
		{"usage ignored when provider does not report", evalMissingMeasured(nil), true, false, false, true, false, ReasonNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := evalCaseReason(c.r, c.ok, true /* execute */, c.reportsUsage, c.priced, c.wantNew, c.wantFail)
			if got != c.want {
				t.Errorf("got %v (%q), want %v", got, got, c.want)
			}
		})
	}
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
