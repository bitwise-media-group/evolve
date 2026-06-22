// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package telemetry

import (
	"errors"

	"go.opentelemetry.io/otel/metric"
)

// instruments holds the metric instruments the reporter decorator records into.
// They derive from the figures the engine already aggregates per item and unit
// (plan.ItemMetrics / run.UnitSummary). All attribute sets stay low-cardinality
// (skill, provider, model, kind, status) — the per-case label is never a metric
// attribute.
type instruments struct {
	caseDuration      metric.Float64Histogram
	caseInputTokens   metric.Int64Histogram
	caseOutputTokens  metric.Int64Histogram
	caseCacheRead     metric.Int64Histogram
	caseCacheCreation metric.Int64Histogram
	caseCost          metric.Float64Histogram
	caseAssertPassed  metric.Int64Histogram
	caseResult        metric.Int64Counter
	triggerHitRate    metric.Float64Histogram
	unitDuration      metric.Float64Histogram
}

// newInstruments builds every instrument from m. The metric API returns a
// usable no-op instrument even on error, so the joined error is advisory: the
// decorator records into whatever it gets back.
func newInstruments(m metric.Meter) (instruments, error) {
	var errs []error
	f64 := func(name, unit, desc string) metric.Float64Histogram {
		h, err := m.Float64Histogram(name, metric.WithUnit(unit), metric.WithDescription(desc))
		errs = append(errs, err)
		return h
	}
	i64 := func(name, unit, desc string) metric.Int64Histogram {
		h, err := m.Int64Histogram(name, metric.WithUnit(unit), metric.WithDescription(desc))
		errs = append(errs, err)
		return h
	}

	ins := instruments{
		caseDuration:      f64("evolve.case.duration", "s", "Wall-clock duration of one trigger query or eval."),
		caseInputTokens:   i64("evolve.case.input_tokens", "{token}", "Fresh (uncached) input tokens for one case."),
		caseOutputTokens:  i64("evolve.case.output_tokens", "{token}", "Output tokens for one eval."),
		caseCacheRead:     i64("evolve.case.cache_read_tokens", "{token}", "Cache-read input tokens for one eval."),
		caseCacheCreation: i64("evolve.case.cache_creation_tokens", "{token}", "Cache-creation input tokens for one eval."),
		caseCost:          f64("evolve.case.cost", "USD", "Estimated USD cost of one case."),
		caseAssertPassed:  i64("evolve.case.assertions_passed", "{assertion}", "Assertions passed in one eval."),
		triggerHitRate:    f64("evolve.trigger.hit_rate", "1", "Trigger hit rate (hits/runs) for one query."),
		unitDuration:      f64("evolve.unit.duration", "s", "Average run duration across a unit's cases."),
	}
	counter, err := m.Int64Counter("evolve.case.result",
		metric.WithUnit("{case}"),
		metric.WithDescription("Finished cases, labeled by outcome status."))
	errs = append(errs, err)
	ins.caseResult = counter

	return ins, errors.Join(errs...)
}
