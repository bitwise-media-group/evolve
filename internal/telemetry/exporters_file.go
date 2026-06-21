// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package telemetry

import (
	"fmt"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// fileExporters opens one JSON file per signal under cfg.Dir and wires the
// stdout exporters to them. Each signal owns its own file, so the trace, metric,
// and log pipelines never contend on a shared writer. The files default to
// compact (one JSON value per line) — pretty-printing would split a record
// across writes — which is the shape otel-tui's --from-json-file expects. The
// returned closers close the files; the caller invokes them after the providers
// have flushed.
func fileExporters(cfg Config) (signalExporters, error) {
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return signalExporters{}, fmt.Errorf("create telemetry dir %s: %w", cfg.Dir, err)
	}

	var opened []*os.File
	closeOpened := func() {
		for _, f := range opened {
			_ = f.Close()
		}
	}
	create := func(name string) (*os.File, error) {
		f, err := os.Create(filepath.Join(cfg.Dir, name))
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", name, err)
		}
		opened = append(opened, f)
		return f, nil
	}

	traceFile, err := create("traces.json")
	if err != nil {
		return signalExporters{}, err
	}
	metricFile, err := create("metrics.json")
	if err != nil {
		closeOpened()
		return signalExporters{}, err
	}
	logFile, err := create("logs.json")
	if err != nil {
		closeOpened()
		return signalExporters{}, err
	}

	span, err := stdouttrace.New(stdouttrace.WithWriter(traceFile))
	if err != nil {
		closeOpened()
		return signalExporters{}, fmt.Errorf("trace exporter: %w", err)
	}
	mexp, err := stdoutmetric.New(stdoutmetric.WithWriter(metricFile))
	if err != nil {
		closeOpened()
		return signalExporters{}, fmt.Errorf("metric exporter: %w", err)
	}
	logs, err := stdoutlog.New(stdoutlog.WithWriter(logFile))
	if err != nil {
		closeOpened()
		return signalExporters{}, fmt.Errorf("log exporter: %w", err)
	}

	return signalExporters{
		span:    span,
		reader:  sdkmetric.NewPeriodicReader(mexp),
		logs:    logs,
		closers: []func() error{traceFile.Close, metricFile.Close, logFile.Close},
	}, nil
}
