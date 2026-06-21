// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	otellog "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// scopeName is the instrumentation scope for telemetry's own logger bridge and
// the decorator's meter; the engine packages use their own package paths.
const scopeName = "github.com/bitwise-media-group/evolve"

// ShutdownFunc flushes the providers and closes any open files. It is safe to
// call once; later calls are no-ops.
type ShutdownFunc func(context.Context) error

// Provider is the result of Init: the logger every subcommand should adopt and
// the mode that was selected.
type Provider struct {
	Logger *slog.Logger
	Mode   Mode
}

// signalExporters carries the per-signal exporters a mode produced, plus any
// file closers to run after the providers flush.
type signalExporters struct {
	span    sdktrace.SpanExporter
	reader  sdkmetric.Reader
	logs    sdklog.Exporter
	closers []func() error
}

// active is the process-wide provider Init installed; WrapReporter consults it
// so it can skip wrapping when telemetry is disabled.
var active *Provider

// Init selects an export mode (file when Config.Dir is set, else env when
// OTEL_* is set, else disabled), installs the global Tracer/Meter/Logger
// providers, and returns the logger plus a shutdown that flushes them. It never
// fails the CLI: on a setup error it returns a working stderr-only logger and a
// no-op shutdown alongside the error, leaving telemetry disabled.
func Init(ctx context.Context, cfg Config) (*Provider, ShutdownFunc, error) {
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	base := slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: cfg.Level})

	mode := resolveMode(cfg.Dir, os.Getenv)
	if mode == ModeDisabled {
		return installDisabled(base, nil)
	}

	var (
		exp signalExporters
		err error
	)
	switch mode {
	case ModeFile:
		exp, err = fileExporters(cfg)
	case ModeEnv:
		exp, err = envExporters(ctx)
	}
	if err != nil {
		return installDisabled(base, err)
	}

	// resource.New may return a best-effort warning; the resource is still
	// usable, so the error is intentionally ignored.
	res, _ := buildResource(ctx, cfg)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exp.span),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(exp.reader),
	)
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exp.logs)),
	)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otellog.SetLoggerProvider(lp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	bridge := otelslog.NewHandler(scopeName, otelslog.WithLoggerProvider(lp))
	logger := slog.New(newFanoutHandler(base, bridge))
	slog.SetDefault(logger)

	prov := &Provider{Logger: logger, Mode: mode}
	active = prov
	return prov, makeShutdown(tp, mp, lp, exp.closers), nil
}

// installDisabled returns a stderr-only provider; err is the setup failure that
// forced the fallback, or nil for a deliberately disabled run.
func installDisabled(base slog.Handler, err error) (*Provider, ShutdownFunc, error) {
	logger := slog.New(base)
	slog.SetDefault(logger)
	prov := &Provider{Logger: logger, Mode: ModeDisabled}
	active = prov
	return prov, noopShutdown, err
}

// makeShutdown flushes the providers in order (tracer, meter, logger) so the
// exporters drain to their files before the files close. The returned func runs
// at most once.
func makeShutdown(tp *sdktrace.TracerProvider, mp *sdkmetric.MeterProvider, lp *sdklog.LoggerProvider,
	closers []func() error,
) ShutdownFunc {
	var once sync.Once
	return func(ctx context.Context) error {
		var err error
		once.Do(func() {
			errs := make([]error, 0, 3+len(closers))
			errs = append(errs, tp.Shutdown(ctx), mp.Shutdown(ctx), lp.Shutdown(ctx))
			for _, c := range closers {
				errs = append(errs, c())
			}
			err = errors.Join(errs...)
		})
		return err
	}
}

func noopShutdown(context.Context) error { return nil }
