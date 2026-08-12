// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import (
	"bufio"
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// call records one Reporter invocation, normalized for comparison.
type call struct {
	Method string
	Unit   plan.UnitRef
	Start  run.ItemStart
	Result run.ItemResult
	Sum    run.UnitSummary
	Msg    string
}

// recorder implements run.Reporter by appending calls.
type recorder struct{ calls []call }

func (r *recorder) UnitStarted(u plan.UnitRef, total, runs int, mode plan.Mode) {
	r.calls = append(r.calls, call{Method: "UnitStarted", Unit: u,
		Start: run.ItemStart{Index: total, Runs: runs}, Msg: fmt.Sprint(mode)})
}
func (r *recorder) UnitSkipped(u plan.UnitRef, reason string) {
	r.calls = append(r.calls, call{Method: "UnitSkipped", Unit: u, Msg: reason})
}
func (r *recorder) ItemStarted(u plan.UnitRef, item run.ItemStart) {
	r.calls = append(r.calls, call{Method: "ItemStarted", Unit: u, Start: item})
}
func (r *recorder) ItemDone(u plan.UnitRef, item run.ItemResult) {
	r.calls = append(r.calls, call{Method: "ItemDone", Unit: u, Result: item})
}
func (r *recorder) BaselineStarted(u plan.UnitRef, item run.ItemStart) {
	r.calls = append(r.calls, call{Method: "BaselineStarted", Unit: u, Start: item})
}
func (r *recorder) BaselineDone(u plan.UnitRef, item run.ItemResult) {
	r.calls = append(r.calls, call{Method: "BaselineDone", Unit: u, Result: item})
}
func (r *recorder) UnitFinished(u plan.UnitRef, sum run.UnitSummary, _ string) {
	r.calls = append(r.calls, call{Method: "UnitFinished", Unit: u, Sum: sum})
}
func (r *recorder) Warn(format string, a ...any) {
	r.calls = append(r.calls, call{Method: "Warn", Msg: fmt.Sprintf(format, a...)})
}

// TestReporterRoundTrip is the load-bearing seam test: every Reporter call
// serialized by EventReporter and replayed through ApplyEvent must land on
// the far Reporter with identical arguments (minus local paths, which the
// wire deliberately drops).
func TestReporterRoundTrip(t *testing.T) {
	u := plan.UnitRef{Skill: "workflow-commit", Key: "anthropic/claude-sonnet-5", Kind: plan.KindEvals}
	hits, runs, ap, at := 2, 3, 4, 5
	avg := 8.25
	cost := 0.75

	drive := func(rep run.Reporter) {
		rep.UnitStarted(u, 7, 1, plan.ModeRun)
		rep.ItemStarted(u, run.ItemStart{Index: 0, Label: "basic-commit", Runs: 1})
		rep.BaselineStarted(u, run.ItemStart{Index: 0, Label: "basic-commit", Runs: 1})
		rep.BaselineDone(u, run.ItemResult{
			Index: 0, Label: "basic-commit", Status: plan.StatusPass,
			Metrics: plan.ItemMetrics{AvgRunSeconds: &avg, CostUSD: &cost},
		})
		rep.ItemDone(u, run.ItemResult{
			Index: 0, Label: "basic-commit", Status: plan.StatusFail, Detail: "2/4 assertions",
			Output: "trailing agent output",
			Metrics: plan.ItemMetrics{
				Hits: &hits, Runs: &runs, AssertPassed: &ap, AssertTotal: &at,
			},
		})
		rep.UnitSkipped(u, "no applicable cases")
		rep.Warn("judge unavailable: %s", "budget")
		rep.UnitFinished(u, run.UnitSummary{Executed: true, Passed: 6, Errored: 1, Total: 7, AvgRunSeconds: &avg}, "evals/x/results.json")
	}

	// Local: drive the recorder directly.
	var want recorder
	drive(&want)

	// Remote: drive the event reporter, then replay the decoded stream.
	var buf bytes.Buffer
	drive(NewEventReporter(&buf, "workflow"))
	var got recorder
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		ev, ok := Decode(sc.Bytes())
		if !ok {
			t.Fatalf("undecodable line: %q", sc.Text())
		}
		ApplyEvent(&got, ev)
	}

	if len(got.calls) != len(want.calls) {
		t.Fatalf("replayed %d calls, want %d", len(got.calls), len(want.calls))
	}
	for i := range want.calls {
		w, g := want.calls[i], got.calls[i]
		// UnitFinished's savedRel is local-only by design.
		if !reflect.DeepEqual(w, g) {
			t.Errorf("call %d (%s):\n got %+v\nwant %+v", i, w.Method, g, w)
		}
	}
}

func TestApplyEventIgnoresResultAndFatal(t *testing.T) {
	var rec recorder
	ApplyEvent(&rec, Event{Type: TypeResult, Result: &UnitResult{Tier: 2}})
	ApplyEvent(&rec, Event{Type: TypeFatal, Error: "boom"})
	if len(rec.calls) != 0 {
		t.Errorf("result/fatal reached the reporter: %+v", rec.calls)
	}
}
