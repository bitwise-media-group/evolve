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
	"github.com/bitwise-media-group/evolve/internal/profile"
	"github.com/bitwise-media-group/evolve/internal/telemetry"
	"github.com/bitwise-media-group/evolve/internal/version"
)

// RootFlags holds the global flags that live outside cli.Options; the rest of
// the persistent flags bind straight into opts.
type RootFlags struct {
	// Verbose raises the log level to debug.
	Verbose bool
	// Profile is the hidden --profile flag: which pprof profiles to write for the
	// run (cpu, memory, all), repeatable or comma-separated.
	Profile []string
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

	// profileStop finalizes any pprof profiles started in PersistentPreRunE; main
	// calls it after the command returns. nil when --profile was not set.
	profileStop profile.StopFunc

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
			// Profiling starts last so the CPU profile brackets the command's work,
			// not the setup above. A bad --profile value (or an unwritable target) is
			// a real error on a flag the user explicitly set, so it fails the run.
			kinds, err := profile.Parse(rootFlags.Profile)
			if err != nil {
				return err
			}
			stop, paths, err := profile.Start(kinds, "")
			if err != nil {
				return err
			}
			profileStop = stop
			if len(paths) > 0 {
				opts.Log.LogAttrs(cmd.Context(), slog.LevelInfo, "profiling enabled",
					slog.Any("files", paths))
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
	// --profile is a developer diagnostic: write pprof profiles for the run.
	// StringSlice accepts both repeated flags and comma-separated lists; profile.Parse
	// resolves cpu/memory/all. Hidden so it stays off the user-facing help and docs.
	pf.StringSliceVar(&rootFlags.Profile, "profile", nil,
		"write pprof profiles for the run: cpu, memory, or all (repeatable or comma-separated)")
	_ = pf.MarkHidden("profile")
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := rootCmd.ExecuteContext(ctx)

	// Finalize profiling first so the profile captures the command, not the
	// telemetry flush below. Like that flush, it lives here rather than in
	// PersistentPostRunE because a RunE error skips PostRun and os.Exit skips defers.
	if profileStop != nil {
		if perr := profileStop(); perr != nil {
			opts.Log.LogAttrs(ctx, slog.LevelWarn, "profiling did not finalize cleanly",
				slog.Any("error", perr))
		}
	}

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
