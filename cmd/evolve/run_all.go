// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/bitwise-media-group/evolve/internal/cli"
)

// allFlags is flag storage only: `run all` never reads it. Values reach the
// tiers through forwardFlags, which copies just the flags the user set, so
// each tier keeps its own defaults (timeout: 120 triggers, 600 evals).
var allFlags = TriggersFlags{}

var runAllCmd = &cobra.Command{
	Use:   "all",
	Short: "Run everything: checks, triggers, evals, then regenerate reports",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		var failures bool
		for _, sub := range []*cobra.Command{checksCmd, triggersCmd, evalsCmd, reportCmd} {
			sub.SetContext(cmd.Context())
			if err := forwardFlags(cmd.Flags(), sub.Flags()); err != nil {
				return err
			}
			if err := sub.RunE(sub, nil); err != nil {
				if errors.Is(err, cli.ErrFailures) {
					failures = true // keep going: later tiers still produce signal
					continue
				}
				return err
			}
		}
		if failures {
			return cli.ErrFailures
		}
		return nil
	},
}

// forwardFlags applies the flags explicitly set on `run all` to one tier's
// flag set, skipping names the tier does not define (--runs is triggers-only;
// checks and report take none of them).
func forwardFlags(from, to *pflag.FlagSet) error {
	var err error
	from.Visit(func(f *pflag.Flag) {
		if err != nil || to.Lookup(f.Name) == nil {
			return
		}
		err = to.Set(f.Name, f.Value.String())
	})
	return err
}

func init() {
	allFlags.register(runAllCmd, 600)
	runAllCmd.Flags().IntVar(&allFlags.Runs, "runs", 3, "runs per query (triggers tier)")
	runAllCmd.Flags().Lookup("timeout").DefValue = "120 triggers, 600 evals"
}
