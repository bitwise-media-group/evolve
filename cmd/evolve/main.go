// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Command evolve evaluates coding-agent plugins: static checks, trigger
// accuracy, behavioral evals, and Markdown/JSON reports.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/bitwise-media-group/evolve/internal/cli"
	"github.com/bitwise-media-group/evolve/internal/telemetry"
	"github.com/bitwise-media-group/evolve/internal/version"
)

// RootFlags holds the global flags that live outside cli.Options; the rest of
// the persistent flags bind straight into opts.
type RootFlags struct {
	// Verbose raises the log level to debug.
	Verbose bool
}

var (
	rootFlags = RootFlags{}

	// logLevel starts at info; --verbose raises it to debug before any
	// subcommand runs.
	logLevel = new(slog.LevelVar)

	// logger writes diagnostics to stderr so stdout stays reserved for
	// command output (tables, JSONL progress).
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	// opts carries the resolved global state every subcommand consumes.
	opts = &cli.Options{Log: logger, Viper: viper.New()}

	// telemetryShutdown flushes the OTEL providers; main calls it after the
	// command returns. Init sets it in PersistentPreRunE.
	telemetryShutdown telemetry.ShutdownFunc

	rootCmd = &cobra.Command{
		Use:           "evolve",
		Short:         "Evaluate coding-agent plugins: static checks, trigger accuracy, behavioral evals, reports",
		SilenceUsage:  true, // errors are failures, not usage mistakes
		SilenceErrors: true, // main logs the error once
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if rootFlags.Verbose {
				logLevel.Set(slog.LevelDebug)
			}
			if err := opts.LoadConfig(cmd); err != nil {
				return err
			}
			// Telemetry resolves after config so --telemetry-dir / telemetry.dir /
			// EVOLVE_TELEMETRY_DIR are all in play. Init never fails the run: a
			// setup error leaves telemetry disabled and is logged, not returned.
			prov, shutdown, err := telemetry.Init(cmd.Context(), telemetry.Config{
				Dir:            opts.TelemetryDir,
				Level:          logLevel,
				ServiceName:    "evolve",
				ServiceVersion: version.Version,
				Stderr:         os.Stderr,
			})
			telemetryShutdown = shutdown
			opts.Log = prov.Logger
			if err != nil {
				prov.Logger.LogAttrs(cmd.Context(), slog.LevelWarn, "telemetry disabled",
					slog.Any("error", err))
			}
			return nil
		},
	}
)

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&opts.Root, "root", "",
		"repository root to operate on (default: walk up from the current directory)")
	pf.StringVar(&opts.Layout, "layout", "auto",
		"repository layout: auto, marketplace, multi, or single")
	pf.BoolVar(&opts.JSON, "json", false, "emit machine-readable JSONL progress on stdout")
	pf.StringVar(&opts.ResultsFormat, "results-format", "",
		"format for results files and the EVALUATION rollup: json, jsonc, or yaml (default: config results_format or json)")
	pf.BoolVarP(&rootFlags.Verbose, "verbose", "v", false, "enable debug logging")
	pf.StringVar(&opts.TelemetryDir, "telemetry-dir", "",
		"write OpenTelemetry traces/metrics/logs as JSON to this directory (default: off; overrides OTEL_* env vars)")
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := rootCmd.ExecuteContext(ctx)

	exitCode := 0
	switch {
	case err == nil:
	case errors.Is(err, cli.ErrFailures):
		exitCode = 1
	default:
		// Logged before shutdown so file-mode telemetry captures the fatal.
		opts.Log.LogAttrs(ctx, slog.LevelError, "fatal", slog.Any("error", err))
		exitCode = 2
	}

	// PersistentPostRunE is skipped when RunE errors and os.Exit skips defers, so
	// the flush runs here for success and failure alike, on a fresh context the
	// interrupt signal cannot have already cancelled.
	if telemetryShutdown != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = telemetryShutdown(shutdownCtx)
		cancel()
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}
}
