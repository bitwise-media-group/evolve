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

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/bitwise-media-group/evolve/internal/cli"
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

	rootCmd = &cobra.Command{
		Use:           "evolve",
		Short:         "Evaluate coding-agent plugins: static checks, trigger accuracy, behavioral evals, reports",
		SilenceUsage:  true, // errors are failures, not usage mistakes
		SilenceErrors: true, // main logs the error once
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if rootFlags.Verbose {
				logLevel.Set(slog.LevelDebug)
			}
			return opts.LoadConfig(cmd)
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
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := rootCmd.ExecuteContext(ctx)
	switch {
	case err == nil:
	case errors.Is(err, cli.ErrFailures):
		os.Exit(1)
	default:
		logger.LogAttrs(ctx, slog.LevelError, "fatal", slog.Any("error", err))
		os.Exit(2)
	}
}
