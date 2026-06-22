// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// captureHandler is a slog.Handler that records the records it receives, gated
// at its own level so tests can assert per-child level routing through the
// fanout. It is safe for concurrent use.
type captureHandler struct {
	level   slog.Level
	mu      *sync.Mutex
	records *[]slog.Record
	attrs   []slog.Attr
}

func newCaptureHandler(level slog.Level) *captureHandler {
	return &captureHandler{level: level, mu: &sync.Mutex{}, records: &[]slog.Record{}}
}

func (h *captureHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.records = append(*h.records, r)
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *captureHandler) WithGroup(string) slog.Handler { return h }

func (h *captureHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(*h.records)
}

// fakeReporter records which run.Reporter methods the decorator forwarded.
type fakeReporter struct {
	itemDone     int
	baselineDone int
	unitFinished int
	unitSkipped  int
	unitStarted  int
}

func (r *fakeReporter) UnitStarted(plan.UnitRef, int, int, plan.Mode)      { r.unitStarted++ }
func (r *fakeReporter) UnitSkipped(plan.UnitRef, string)                   { r.unitSkipped++ }
func (r *fakeReporter) ItemStarted(plan.UnitRef, run.ItemStart)            {}
func (r *fakeReporter) ItemDone(plan.UnitRef, run.ItemResult)              { r.itemDone++ }
func (r *fakeReporter) BaselineStarted(plan.UnitRef, run.ItemStart)        {}
func (r *fakeReporter) BaselineDone(plan.UnitRef, run.ItemResult)          { r.baselineDone++ }
func (r *fakeReporter) UnitFinished(plan.UnitRef, run.UnitSummary, string) { r.unitFinished++ }
func (r *fakeReporter) Warn(string, ...any)                                {}

// metricByName indexes collected metrics by instrument name for assertions.
func metricByName(t *testing.T, rm metricdata.ResourceMetrics) map[string]metricdata.Metrics {
	t.Helper()
	out := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = m
		}
	}
	return out
}
