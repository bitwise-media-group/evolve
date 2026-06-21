// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/bitwise-media-group/evolve/internal/run"
)

// WrapReporter decorates r so finished items and units also emit OTEL metrics
// and structured (debug) logs, reading the figures the engine already
// aggregated. When telemetry is disabled it returns r unchanged, so a default
// run is byte-for-byte what it was before. The decorator forwards every call to
// r first (preserving its output) and records afterward; metrics and logs alike
// stay off the trace path, since the reporter fires after the work's span has
// ended.
func WrapReporter(r run.Reporter) run.Reporter {
	if active == nil || active.Mode == ModeDisabled {
		return r
	}
	inst, err := newInstruments(otel.Meter(scopeName))
	if err != nil {
		active.Logger.Debug("telemetry: metric instrument setup", slog.Any("error", err))
	}
	return &telemetryReporter{Reporter: r, log: active.Logger, inst: inst}
}

// telemetryReporter forwards to the wrapped reporter and records metrics/logs on
// the finishing events. The embedded run.Reporter supplies the pass-through
// methods (UnitStarted, ItemStarted, BaselineStarted, Warn) unchanged.
type telemetryReporter struct {
	run.Reporter
	log  *slog.Logger
	inst instruments
}

// ItemDone records the case's metrics and a debug log, then is otherwise the
// wrapped reporter's ItemDone.
func (t *telemetryReporter) ItemDone(u run.UnitRef, item run.ItemResult) {
	t.Reporter.ItemDone(u, item)
	t.recordItem(u, item, false)
}

// BaselineDone mirrors ItemDone for the without-skill baseline case.
func (t *telemetryReporter) BaselineDone(u run.UnitRef, item run.ItemResult) {
	t.Reporter.BaselineDone(u, item)
	t.recordItem(u, item, true)
}

// UnitSkipped notes the skip as a debug log alongside the wrapped reporter.
func (t *telemetryReporter) UnitSkipped(u run.UnitRef, reason string) {
	t.Reporter.UnitSkipped(u, reason)
	attrs := append(unitLogAttrs(u), slog.String("reason", reason))
	t.log.LogAttrs(context.Background(), slog.LevelDebug, "unit skipped", attrs...)
}

// UnitFinished records the unit rollup as a histogram sample and a debug log.
func (t *telemetryReporter) UnitFinished(u run.UnitRef, sum run.UnitSummary, savedRel string) {
	t.Reporter.UnitFinished(u, sum, savedRel)
	ctx := context.Background()
	if sum.AvgRunSeconds != nil {
		t.inst.unitDuration.Record(ctx, *sum.AvgRunSeconds, metric.WithAttributeSet(unitAttrSet(u)))
	}
	attrs := append(
		unitLogAttrs(u),
		slog.Bool("executed", sum.Executed),
		slog.Int("passed", sum.Passed),
		slog.Int("failed", sum.Failed),
		slog.Int("errored", sum.Errored),
		slog.Int("total", sum.Total),
	)
	t.log.LogAttrs(ctx, slog.LevelDebug, "unit finished", attrs...)
}

// recordItem records every populated metric for one finished case and a debug
// log. ItemMetrics fields are pointers because triggers and evals fill disjoint
// subsets, so each is guarded before recording.
func (t *telemetryReporter) recordItem(u run.UnitRef, item run.ItemResult, baseline bool) {
	ctx := context.Background()
	set := caseAttrSet(u, item.Status)
	opt := metric.WithAttributeSet(set)

	t.inst.caseResult.Add(ctx, 1, opt)

	m := item.Metrics
	if m.AvgRunSeconds != nil {
		t.inst.caseDuration.Record(ctx, *m.AvgRunSeconds, opt)
	}
	if m.InputTokens != nil {
		t.inst.caseInputTokens.Record(ctx, int64(*m.InputTokens), opt)
	}
	if m.OutputTokens != nil {
		t.inst.caseOutputTokens.Record(ctx, int64(*m.OutputTokens), opt)
	}
	if m.CacheReadTokens != nil {
		t.inst.caseCacheRead.Record(ctx, int64(*m.CacheReadTokens), opt)
	}
	if m.CacheCreationTokens != nil {
		t.inst.caseCacheCreation.Record(ctx, int64(*m.CacheCreationTokens), opt)
	}
	if m.CostUSD != nil {
		t.inst.caseCost.Record(ctx, *m.CostUSD, opt)
	}
	if m.AssertPassed != nil {
		t.inst.caseAssertPassed.Record(ctx, int64(*m.AssertPassed), opt)
	}
	if m.Hits != nil && m.Runs != nil && *m.Runs > 0 {
		t.inst.triggerHitRate.Record(ctx, float64(*m.Hits)/float64(*m.Runs), opt)
	}

	attrs := append(
		unitLogAttrs(u),
		slog.String("status", statusString(item.Status)),
		slog.String("label", item.Label),
		slog.Bool("baseline", baseline),
	)
	t.log.LogAttrs(ctx, slog.LevelDebug, "case done", attrs...)
}

// caseAttrSet is the low-cardinality metric attribute set for one case.
func caseAttrSet(u run.UnitRef, s run.Status) attribute.Set {
	prov, model := splitKey(u.Key)
	return attribute.NewSet(
		attribute.String("skill", u.Skill),
		attribute.String("provider", prov),
		attribute.String("model", model),
		attribute.String("kind", kindString(u.Kind)),
		attribute.String("status", statusString(s)),
	)
}

// unitAttrSet is caseAttrSet without the per-case status, for unit rollups.
func unitAttrSet(u run.UnitRef) attribute.Set {
	prov, model := splitKey(u.Key)
	return attribute.NewSet(
		attribute.String("skill", u.Skill),
		attribute.String("provider", prov),
		attribute.String("model", model),
		attribute.String("kind", kindString(u.Kind)),
	)
}

// unitLogAttrs is the shared slog attribute prefix for a unit's log lines.
func unitLogAttrs(u run.UnitRef) []slog.Attr {
	prov, model := splitKey(u.Key)
	return []slog.Attr{
		slog.String("skill", u.Skill),
		slog.String("provider", prov),
		slog.String("model", model),
		slog.String("kind", kindString(u.Kind)),
	}
}

// splitKey splits a "provider/model" unit key into its parts.
func splitKey(key string) (provider, model string) {
	if before, after, ok := strings.Cut(key, "/"); ok {
		return before, after
	}
	return key, ""
}

// kindString names a unit's tier.
func kindString(k run.Kind) string {
	if k == run.KindTriggers {
		return "triggers"
	}
	return "evals"
}

// statusString names a case outcome.
func statusString(s run.Status) string {
	switch s {
	case run.StatusPass:
		return "pass"
	case run.StatusFail:
		return "fail"
	case run.StatusSkip:
		return "skip"
	case run.StatusError:
		return "error"
	default:
		return "unknown"
	}
}
