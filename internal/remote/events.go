// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import (
	"fmt"
	"io"
	"sync"

	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// kindNames maps plan.Kind onto the wire vocabulary.
var kindNames = map[plan.Kind]string{
	plan.KindTriggers: "triggers",
	plan.KindEvals:    "evals",
}

// kindByName is the inverse, for replay.
var kindByName = map[string]plan.Kind{
	"triggers": plan.KindTriggers,
	"evals":    plan.KindEvals,
}

// statusNames maps plan.Status onto the wire vocabulary.
var statusNames = map[plan.Status]string{
	plan.StatusPass:  "pass",
	plan.StatusFail:  "fail",
	plan.StatusSkip:  "skip",
	plan.StatusError: "error",
}

// statusByName is the inverse, for replay.
var statusByName = map[string]plan.Status{
	"pass": plan.StatusPass, "fail": plan.StatusFail,
	"skip": plan.StatusSkip, "error": plan.StatusError,
}

// modeNames maps plan.Mode onto the wire vocabulary.
var modeNames = map[plan.Mode]string{
	plan.ModeRun:       "run",
	plan.ModeCountOnly: "count-only",
}

// modeByName is the inverse, for replay.
var modeByName = map[string]plan.Mode{
	"run": plan.ModeRun, "count-only": plan.ModeCountOnly,
}

// EventReporter implements run.Reporter by emitting EVOLVE-EVENT lines: the
// in-pod half of the Reporter seam. Emissions are mutex-serialized —
// ItemDone and Warn fire from the parallel agent-run goroutines — and
// everything else the process prints must go to stderr, keeping stdout pure
// event stream.
type EventReporter struct {
	mu     sync.Mutex
	w      io.Writer
	plugin string
}

// NewEventReporter builds an EventReporter writing to w (the pod's stdout).
// plugin tags every unit reference, since plan.UnitRef does not carry it.
func NewEventReporter(w io.Writer, plugin string) *EventReporter {
	return &EventReporter{w: w, plugin: plugin}
}

// Emit writes one event line directly — the result/fatal channel exec-unit
// uses beside the Reporter methods.
func (r *EventReporter) Emit(ev Event) { r.emit(ev) }

// emit writes one event line; marshalling errors are unrecoverable protocol
// bugs and are swallowed after best-effort reporting on the same stream.
func (r *EventReporter) emit(ev Event) {
	line, err := ev.Encode()
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = fmt.Fprintln(r.w, line)
}

// ref renders a plan.UnitRef onto the wire.
func (r *EventReporter) ref(u plan.UnitRef) *UnitRef {
	return &UnitRef{Plugin: r.plugin, Skill: u.Skill, Key: u.Key, Kind: kindNames[u.Kind]}
}

// wireItemStart renders an ItemStart payload.
func wireItemStart(item run.ItemStart) *ItemEvent {
	return &ItemEvent{Index: item.Index, Label: item.Label, Runs: item.Runs}
}

// wireItemResult renders an ItemResult payload. Local workspace and log
// paths never cross the wire — they are meaningless outside the pod.
func wireItemResult(item run.ItemResult) *ItemEvent {
	return &ItemEvent{
		Index:   item.Index,
		Label:   item.Label,
		Status:  statusNames[item.Status],
		Detail:  Truncate(item.Detail, MaxDetailLen),
		Output:  Truncate(item.Output, MaxOutputLen),
		Metrics: wireMetrics(item.Metrics),
	}
}

// wireMetrics renders plan.ItemMetrics; an all-nil metrics block collapses
// to nil.
func wireMetrics(m plan.ItemMetrics) *ItemMetrics {
	out := &ItemMetrics{
		Hits:                m.Hits,
		Runs:                m.Runs,
		AvgRunSeconds:       m.AvgRunSeconds,
		InputTokens:         m.InputTokens,
		CacheReadTokens:     m.CacheReadTokens,
		CacheCreationTokens: m.CacheCreationTokens,
		OutputTokens:        m.OutputTokens,
		CostUSD:             m.CostUSD,
		AssertPassed:        m.AssertPassed,
		AssertTotal:         m.AssertTotal,
	}
	if *out == (ItemMetrics{}) {
		return nil
	}
	return out
}

// planMetrics is wireMetrics' inverse.
func planMetrics(m *ItemMetrics) plan.ItemMetrics {
	if m == nil {
		return plan.ItemMetrics{}
	}
	return plan.ItemMetrics{
		Hits:                m.Hits,
		Runs:                m.Runs,
		AvgRunSeconds:       m.AvgRunSeconds,
		InputTokens:         m.InputTokens,
		CacheReadTokens:     m.CacheReadTokens,
		CacheCreationTokens: m.CacheCreationTokens,
		OutputTokens:        m.OutputTokens,
		CostUSD:             m.CostUSD,
		AssertPassed:        m.AssertPassed,
		AssertTotal:         m.AssertTotal,
	}
}

// UnitStarted implements run.Reporter.
func (r *EventReporter) UnitStarted(u plan.UnitRef, total, runs int, mode plan.Mode) {
	r.emit(Event{Type: TypeUnitStarted, Unit: r.ref(u),
		Item: &ItemEvent{Total: total, Runs: runs, Mode: modeNames[mode]}})
}

// UnitSkipped implements run.Reporter.
func (r *EventReporter) UnitSkipped(u plan.UnitRef, reason string) {
	r.emit(Event{Type: TypeUnitSkipped, Unit: r.ref(u), Msg: reason})
}

// ItemStarted implements run.Reporter.
func (r *EventReporter) ItemStarted(u plan.UnitRef, item run.ItemStart) {
	r.emit(Event{Type: TypeItemStarted, Unit: r.ref(u), Item: wireItemStart(item)})
}

// ItemDone implements run.Reporter.
func (r *EventReporter) ItemDone(u plan.UnitRef, item run.ItemResult) {
	r.emit(Event{Type: TypeItemDone, Unit: r.ref(u), Item: wireItemResult(item)})
}

// BaselineStarted implements run.Reporter.
func (r *EventReporter) BaselineStarted(u plan.UnitRef, item run.ItemStart) {
	r.emit(Event{Type: TypeBaselineStarted, Unit: r.ref(u), Item: wireItemStart(item)})
}

// BaselineDone implements run.Reporter.
func (r *EventReporter) BaselineDone(u plan.UnitRef, item run.ItemResult) {
	r.emit(Event{Type: TypeBaselineDone, Unit: r.ref(u), Item: wireItemResult(item)})
}

// UnitFinished implements run.Reporter. savedRel is dropped: the pod's
// results path is meaningless to the monitor, which reports its own local
// save path when it lands the entry.
func (r *EventReporter) UnitFinished(u plan.UnitRef, sum run.UnitSummary, _ string) {
	r.emit(Event{Type: TypeUnitFinished, Unit: r.ref(u), Sum: &UnitSummary{
		Executed:      sum.Executed,
		Passed:        sum.Passed,
		Failed:        sum.Failed,
		Errored:       sum.Errored,
		Total:         sum.Total,
		AvgRunSeconds: sum.AvgRunSeconds,
	}})
}

// Warn implements run.Reporter.
func (r *EventReporter) Warn(format string, a ...any) {
	r.emit(Event{Type: TypeWarn, Msg: fmt.Sprintf(format, a...)})
}

// planRef recovers a plan.UnitRef from the wire.
func planRef(u *UnitRef) plan.UnitRef {
	if u == nil {
		return plan.UnitRef{}
	}
	return plan.UnitRef{Skill: u.Skill, Key: u.Key, Kind: kindByName[u.Kind]}
}

// planItemStart recovers a run.ItemStart.
func planItemStart(item *ItemEvent) run.ItemStart {
	if item == nil {
		return run.ItemStart{}
	}
	return run.ItemStart{Index: item.Index, Label: item.Label, Runs: item.Runs}
}

// planItemResult recovers a run.ItemResult.
func planItemResult(item *ItemEvent) run.ItemResult {
	if item == nil {
		return run.ItemResult{}
	}
	return run.ItemResult{
		Index:   item.Index,
		Label:   item.Label,
		Status:  statusByName[item.Status],
		Detail:  item.Detail,
		Output:  item.Output,
		Metrics: planMetrics(item.Metrics),
	}
}

// ApplyEvent replays one wire event onto a local run.Reporter: the monitor
// half of the seam. result and fatal events are not progress and are
// ignored here — the sweep handles them itself.
func ApplyEvent(rep run.Reporter, ev Event) {
	u := planRef(ev.Unit)
	switch ev.Type {
	case TypeUnitStarted:
		var total, runs int
		mode := plan.ModeRun
		if ev.Item != nil {
			total, runs = ev.Item.Total, ev.Item.Runs
			mode = modeByName[ev.Item.Mode]
		}
		rep.UnitStarted(u, total, runs, mode)
	case TypeUnitSkipped:
		rep.UnitSkipped(u, ev.Msg)
	case TypeItemStarted:
		rep.ItemStarted(u, planItemStart(ev.Item))
	case TypeItemDone:
		rep.ItemDone(u, planItemResult(ev.Item))
	case TypeBaselineStarted:
		rep.BaselineStarted(u, planItemStart(ev.Item))
	case TypeBaselineDone:
		rep.BaselineDone(u, planItemResult(ev.Item))
	case TypeUnitFinished:
		var sum run.UnitSummary
		if ev.Sum != nil {
			sum = run.UnitSummary{
				Executed:      ev.Sum.Executed,
				Passed:        ev.Sum.Passed,
				Failed:        ev.Sum.Failed,
				Errored:       ev.Sum.Errored,
				Total:         ev.Sum.Total,
				AvgRunSeconds: ev.Sum.AvgRunSeconds,
			}
		}
		rep.UnitFinished(u, sum, "")
	case TypeWarn:
		if ev.Msg != "" {
			rep.Warn("%s", ev.Msg)
		}
	}
}
