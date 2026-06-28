// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/cli"
	"github.com/bitwise-media-group/evolve/internal/report"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/version"
)

// ReportFlags holds the flags for `evolve report`.
type ReportFlags struct {
	Check               bool
	Migrate             bool
	MinTriggersPassRate float64
	MinEvalsPassRate    float64
	JUnit               string
	Cobertura           string
	Strict              bool
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
		if reportFlags.Migrate {
			if err := runMigrate(cmd); err != nil {
				return err
			}
		}
		if err := reconcileStaleResults(cmd, interactiveTUI(cmd, opts.JSON)); err != nil {
			return err
		}
		active, _, err := opts.ActiveModelKeys()
		if err != nil {
			return err
		}
		junit := opts.JUnitPath()
		if cmd.Flags().Changed("junit") {
			junit = reportFlags.JUnit
		}
		cobertura := opts.CoberturaPath()
		if cmd.Flags().Changed("cobertura") {
			cobertura = reportFlags.Cobertura
		}
		strict := opts.StrictConfig()
		if cmd.Flags().Changed("strict") {
			strict = reportFlags.Strict
		}
		var coverage []report.SkillCoverage
		if cobertura != "" {
			if coverage, err = opts.Coverage(repo, strict); err != nil {
				return err
			}
		}
		summary, err := report.Generate(report.Options{
			Repo:          repo,
			ToolVersion:   version.Version,
			Models:        models,
			Format:        opts.ResultsFormat,
			ActiveModels:  active,
			JUnitPath:     junit,
			CoberturaPath: cobertura,
			Coverage:      coverage,
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
		if strict {
			th.Strict = true
			if th.Defined, err = opts.DefinedModelKeys(); err != nil {
				return err
			}
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

// runMigrate upgrades every stored results file to the current schema before the
// report is generated, so a structural schema bump lands in the committed files
// without a full eval rerun. It reports each upgraded file and is a no-op once
// every file is current.
func runMigrate(cmd *cobra.Command) error {
	upgraded, err := opts.MigrateResults()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(upgraded) == 0 {
		fmt.Fprintf(out, "migrate: results files already at schema %d\n", results.Schema)
		return nil
	}
	for _, m := range upgraded {
		fmt.Fprintf(out, "migrate: upgraded %s/%s from schema %d to %d\n",
			m.Plugin, m.Skill, m.FromSchema, results.Schema)
	}
	fmt.Fprintf(out, "migrate: upgraded %d results %s to schema %d\n",
		len(upgraded), plural(len(upgraded), "file", "files"), results.Schema)
	return nil
}

func init() {
	reportCmd.Flags().BoolVar(&reportFlags.Check, "check", false,
		"fail when pass rates breach the configured thresholds")
	reportCmd.Flags().BoolVar(&reportFlags.Migrate, "migrate", false,
		"upgrade stored results files to the latest schema before generating the reports")
	reportCmd.Flags().Float64Var(&reportFlags.MinTriggersPassRate, "min-triggers-pass-rate", 0,
		"minimum trigger pass rate (0..1) for --check")
	reportCmd.Flags().Float64Var(&reportFlags.MinEvalsPassRate, "min-evals-pass-rate", 0,
		"minimum eval pass rate (0..1) for --check")
	reportCmd.Flags().StringVar(&reportFlags.JUnit, "junit", "",
		"also write a JUnit XML test-results file to this path (overrides report.junit)")
	reportCmd.Flags().StringVar(&reportFlags.Cobertura, "cobertura", "",
		"also write a Cobertura XML coverage file to this path (overrides report.cobertura)")
	reportCmd.Flags().BoolVar(&reportFlags.Strict, "strict", false,
		"require the configured model matrix: --check holds every defined model to the thresholds, "+
			"and --cobertura covers a skill only when every defined model has a current result")
	reportCmd.Flags().String("stale-results", "",
		"keep|drop stored results for models outside the models restriction (default: prompt on a terminal, else keep)")
	rootCmd.AddCommand(reportCmd)
}
