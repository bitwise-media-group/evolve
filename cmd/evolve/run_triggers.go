// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/grade"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// TriggersFlags holds the flags for `evolve run triggers`.
type TriggersFlags struct {
	SweepFlags
	Runs int
}

var triggersFlags = TriggersFlags{}

var triggersCmd = &cobra.Command{
	Use:   "triggers",
	Short: "Run Tier 1 trigger-accuracy evals through headless agent sessions",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		interactive := interactiveTUI(cmd, triggersFlags.NoTUI)
		if err := reconcileStaleResults(cmd, interactive); err != nil {
			return err
		}
		if interactive {
			return uiRun(cmd, &triggersFlags.SweepFlags, plan.Tiers{Triggers: true},
				triggersFlags.Runs, "", grade.DefaultJudgeModel, "triggers: some queries failed", false)
		}

		common, err := triggersFlags.sweepOptions(cmd)
		if err != nil {
			return err
		}

		if !triggersFlags.CountOnly {
			fmt.Fprintf(cmd.OutOrStdout(), "parallelism: %d concurrent agent runs\n", triggersFlags.Jobs)
		}
		failed, runErr := run.Triggers(cmd.Context(), run.TriggerOptions{
			Options: common,
			Runs:    triggersFlags.Runs,
		})
		if err := saveCounter(common.Counter); err != nil {
			return err
		}
		if runErr != nil {
			return runErr
		}
		if err := opts.RegenerateReports(); err != nil {
			return err
		}
		if failed {
			return failOrWarn(cmd, "triggers: some queries failed")
		}
		return nil
	},
}

func init() {
	triggersFlags.register(triggersCmd, 120)
	triggersCmd.Flags().IntVar(&triggersFlags.Runs, "runs", 3, "runs per query")
	runCmd.AddCommand(triggersCmd)
}
