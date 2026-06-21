// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestFileExportersWriteEachSignal drives local providers built from the file
// exporters (not the globals) so the assertion is deterministic regardless of
// test order, and confirms each signal lands JSON in its own file.
func TestFileExportersWriteEachSignal(t *testing.T) {
	dir := t.TempDir()
	exp, err := fileExporters(Config{Dir: dir})
	if err != nil {
		t.Fatalf("fileExporters: %v", err)
	}

	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp.span))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exp.reader))
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(exp.logs)))

	ctx := context.Background()
	_, span := tp.Tracer("test").Start(ctx, "evolve.test.span")
	span.End()

	counter, err := mp.Meter("test").Int64Counter("evolve.test.counter")
	if err != nil {
		t.Fatalf("counter: %v", err)
	}
	counter.Add(ctx, 1)

	logger := slog.New(otelslog.NewHandler("test", otelslog.WithLoggerProvider(lp)))
	logger.InfoContext(ctx, "evolve test log", slog.String("k", "v"))

	for _, shutdown := range []func(context.Context) error{tp.Shutdown, mp.Shutdown, lp.Shutdown} {
		if err := shutdown(ctx); err != nil {
			t.Errorf("provider shutdown: %v", err)
		}
	}
	for _, closeFile := range exp.closers {
		if err := closeFile(); err != nil {
			t.Errorf("close file: %v", err)
		}
	}

	for _, name := range []string{"traces.json", "metrics.json", "logs.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if n := countJSONValues(t, data); n == 0 {
			t.Errorf("%s holds no JSON values (%d bytes)", name, len(data))
		}
	}
}

// countJSONValues decodes the file as a stream of JSON values, tolerating both
// newline-delimited and concatenated encodings.
func countJSONValues(t *testing.T, data []byte) int {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	n := 0
	for {
		var v any
		err := dec.Decode(&v)
		if err == io.EOF {
			return n
		}
		if err != nil {
			t.Errorf("decode JSON stream: %v", err)
			return n
		}
		n++
	}
}
