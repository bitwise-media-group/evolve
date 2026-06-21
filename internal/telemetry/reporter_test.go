// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"log/slog"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/bitwise-media-group/evolve/internal/run"
)

func TestWrapReporterDisabledReturnsInput(t *testing.T) {
	active = &Provider{Mode: ModeDisabled, Logger: slog.Default()}
	fake := &fakeReporter{}
	if got := WrapReporter(fake); got != run.Reporter(fake) {
		t.Errorf("disabled WrapReporter should return the input unchanged, got %T", got)
	}
}

func TestWrapReporterEnabledWraps(t *testing.T) {
	active = &Provider{Mode: ModeFile, Logger: slog.New(newCaptureHandler(slog.LevelDebug))}
	fake := &fakeReporter{}
	got := WrapReporter(fake)
	if _, ok := got.(*telemetryReporter); !ok {
		t.Fatalf("enabled WrapReporter should wrap, got %T", got)
	}
	got.ItemDone(run.UnitRef{Skill: "s", Key: "anthropic/m", Kind: run.KindEvals}, run.ItemResult{})
	if fake.itemDone != 1 {
		t.Errorf("ItemDone not forwarded to the wrapped reporter: %d", fake.itemDone)
	}
}

func TestTelemetryReporterRecordsMetricsAndLogs(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := newInstruments(mp.Meter("test"))
	if err != nil {
		t.Fatalf("newInstruments: %v", err)
	}
	logCap := newCaptureHandler(slog.LevelDebug)
	fake := &fakeReporter{}
	tr := &telemetryReporter{Reporter: fake, log: slog.New(logCap), inst: inst}

	ref := run.UnitRef{Skill: "skill", Key: "anthropic/claude", Kind: run.KindEvals}
	dur, in := 1.5, 100
	tr.ItemDone(ref, run.ItemResult{
		Index: 0, Label: "case-1", Status: run.StatusPass,
		Metrics: run.ItemMetrics{AvgRunSeconds: &dur, InputTokens: &in},
	})
	unitAvg := 2.0
	tr.UnitFinished(ref, run.UnitSummary{Executed: true, Passed: 1, Total: 1, AvgRunSeconds: &unitAvg}, "results.json")

	if fake.itemDone != 1 || fake.unitFinished != 1 {
		t.Errorf("forwarding wrong: itemDone=%d unitFinished=%d", fake.itemDone, fake.unitFinished)
	}
	if logCap.count() != 2 {
		t.Errorf("want a debug log for the item and the unit, got %d", logCap.count())
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	metrics := metricByName(t, rm)
	for _, name := range []string{"evolve.case.result", "evolve.case.duration", "evolve.case.input_tokens", "evolve.unit.duration"} {
		if _, ok := metrics[name]; !ok {
			t.Errorf("missing metric %q", name)
		}
	}
	// Output tokens were nil, so the histogram must not have been recorded.
	if _, ok := metrics["evolve.case.output_tokens"]; ok {
		t.Error("recorded output_tokens histogram for a nil field")
	}
	// The result counter saw exactly one case.
	if m, ok := metrics["evolve.case.result"]; ok {
		sum, ok := m.Data.(metricdata.Sum[int64])
		if !ok {
			t.Fatalf("evolve.case.result is %T, want Sum[int64]", m.Data)
		}
		var total int64
		for _, dp := range sum.DataPoints {
			total += dp.Value
		}
		if total != 1 {
			t.Errorf("case.result total = %d, want 1", total)
		}
	}
}
