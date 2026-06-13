// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/grade"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// EvalsFlags holds the flags for `evolve run evals`.
type EvalsFlags struct {
	SweepFlags
	Eval       string
	JudgeModel string
}

var evalsFlags = EvalsFlags{}

var evalsCmd = &cobra.Command{
	Use:   "evals",
	Short: "Run Tier 2 behavioral evals: agent sessions graded by assertions",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		common, err := evalsFlags.sweepOptions(cmd)
		if err != nil {
			return err
		}

		if !evalsFlags.CountOnly {
			fmt.Fprintf(cmd.OutOrStdout(), "parallelism: %d concurrent evals\n", evalsFlags.Jobs)
		}
		failed, runErr := run.Evals(cmd.Context(), run.EvalOptions{
			Options:    common,
			EvalFilter: evalsFlags.Eval,
			JudgeModel: evalsFlags.JudgeModel,
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
			return failOrWarn(cmd, "evals: some evals failed")
		}
		return nil
	},
}

func init() {
	evalsFlags.register(evalsCmd, 600)
	evalsCmd.Flags().StringVar(&evalsFlags.Eval, "eval", "", "only run the eval with this id")
	evalsCmd.Flags().StringVar(&evalsFlags.JudgeModel, "judge-model", grade.DefaultJudgeModel,
		"claude model that grades llm assertions")
	runCmd.AddCommand(evalsCmd)
}
