// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/cli"
	"github.com/bitwise-media-group/evolve/internal/report"
	"github.com/bitwise-media-group/evolve/internal/version"
)

// ReportFlags holds the flags for `evolve report`.
type ReportFlags struct {
	Check               bool
	MinTriggersPassRate float64
	MinEvalsPassRate    float64
}

var reportFlags = ReportFlags{}

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Regenerate EVALUATION.md and EVALUATION.json from the stored results",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		repo, err := opts.Repo()
		if err != nil {
			return err
		}
		models, err := opts.AvailableModels()
		if err != nil {
			return err
		}
		if err := reconcileStaleResults(cmd, interactiveTUI(cmd, opts.JSON)); err != nil {
			return err
		}
		active, _, err := opts.ActiveModelKeys()
		if err != nil {
			return err
		}
		summary, err := report.Generate(report.Options{
			Repo:         repo,
			ToolVersion:  version.Version,
			Models:       models,
			Format:       opts.ResultsFormat,
			ActiveModels: active,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "report: regenerated EVALUATION.md and %s (%d plugins)\n",
			report.SummaryName(opts.ResultsFormat), len(summary.Plugins))

		if !reportFlags.Check {
			return nil
		}
		th := opts.Thresholds()
		if cmd.Flags().Changed("min-triggers-pass-rate") {
			th.TriggersMinPassRate = &reportFlags.MinTriggersPassRate
		}
		if cmd.Flags().Changed("min-evals-pass-rate") {
			th.EvalsMinPassRate = &reportFlags.MinEvalsPassRate
		}
		if th.TriggersMinPassRate == nil && th.EvalsMinPassRate == nil {
			return fmt.Errorf("report --check: no thresholds configured " +
				"(set report.thresholds in the .evolve config file or pass --min-*-pass-rate flags)")
		}
		breaches := report.Check(summary, th)
		for _, breach := range breaches {
			fmt.Fprintf(cmd.ErrOrStderr(), "FAIL: %s\n", breach)
		}
		if len(breaches) > 0 {
			return fmt.Errorf("report: %d threshold %s: %w",
				len(breaches), plural(len(breaches), "breach", "breaches"), cli.ErrFailures)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "report: thresholds met")
		return nil
	},
}

func init() {
	reportCmd.Flags().BoolVar(&reportFlags.Check, "check", false,
		"fail when pass rates breach the configured thresholds")
	reportCmd.Flags().Float64Var(&reportFlags.MinTriggersPassRate, "min-triggers-pass-rate", 0,
		"minimum trigger pass rate (0..1) for --check")
	reportCmd.Flags().Float64Var(&reportFlags.MinEvalsPassRate, "min-evals-pass-rate", 0,
		"minimum eval pass rate (0..1) for --check")
	reportCmd.Flags().String("stale-results", "",
		"keep|drop stored results for models outside the models restriction (default: prompt on a terminal, else keep)")
	rootCmd.AddCommand(reportCmd)
}
