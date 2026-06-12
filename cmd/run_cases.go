// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/grade"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// CasesFlags holds the flags for `evolve run cases`.
type CasesFlags struct {
	SweepFlags
	Case       string
	JudgeModel string
}

var casesFlags = CasesFlags{}

var casesCmd = &cobra.Command{
	Use:   "cases",
	Short: "Run Tier 2 behavioral evals: agent sessions graded by assertions",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		common, err := casesFlags.sweepOptions(cmd)
		if err != nil {
			return err
		}

		if !casesFlags.CountOnly {
			fmt.Fprintf(cmd.OutOrStdout(), "parallelism: %d concurrent cases\n", casesFlags.Jobs)
		}
		failed, runErr := run.Cases(cmd.Context(), run.CaseOptions{
			Options:    common,
			CaseFilter: casesFlags.Case,
			JudgeModel: casesFlags.JudgeModel,
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
			return failOrWarn(cmd, "cases: some cases failed")
		}
		return nil
	},
}

func init() {
	casesFlags.register(casesCmd, 600)
	casesCmd.Flags().StringVar(&casesFlags.Case, "case", "", "only run the case with this id")
	casesCmd.Flags().StringVar(&casesFlags.JudgeModel, "judge-model", grade.DefaultJudgeModel,
		"claude model that grades llm assertions")
	runCmd.AddCommand(casesCmd)
}
