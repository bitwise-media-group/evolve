// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/run"
)

// ChecksFlags holds the flags for `evolve run checks`.
type ChecksFlags struct {
	NoMarketplace bool
	License       string
	NoSignals     bool
}

var checksFlags = ChecksFlags{}

var checksCmd = &cobra.Command{
	Use:   "checks",
	Short: "Run Tier 0 static checks: skill frontmatter, manifests, marketplace consistency",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		repo, err := opts.Repo()
		if err != nil {
			return err
		}
		cfg := opts.ChecksConfig()
		if checksFlags.NoMarketplace {
			cfg.Marketplace = false
		}
		if checksFlags.NoSignals {
			cfg.Signals.Enabled = false
		}
		if cmd.Flags().Changed("license") {
			cfg.License = checksFlags.License
		}

		findings, err := run.Checks(repo, cfg)
		if err != nil {
			return err
		}
		for _, f := range findings {
			fmt.Fprintf(cmd.ErrOrStderr(), "FAIL: %s\n", f.Message)
		}

		// Gating verdict comes first; the advisory signals follow it so they
		// never read as part of the pass/fail decision. result is captured (not
		// returned early) so the signals print even when --strict fails.
		var result error
		if n := len(findings); n > 0 {
			result = failOrWarn(cmd, "checks: %d %s", n, plural(n, "failure", "failures"))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "checks: all checks passed (%s layout, %d %s)\n",
				repo.Kind, len(repo.Plugins), plural(len(repo.Plugins), "plugin", "plugins"))
		}

		if cfg.Signals.Enabled {
			signals, err := run.Signals(repo, cfg)
			if err != nil {
				return err
			}
			renderSignals(cmd.OutOrStdout(), signals)
		}
		return result
	},
}

// renderSignals prints the non-blocking skill-quality signals as an advisory
// table. It is deliberately separate from the FAIL/verdict output: these are
// 0–100 gradient scores (higher is better), never pass/fail conditions.
func renderSignals(w io.Writer, skills []run.SkillSignals) {
	if len(skills) == 0 {
		return
	}
	fmt.Fprintln(w, "\nskill signals (advisory — informational, does not affect pass/fail):")
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "  SKILL\tOVERALL\tSIZE\tCONCISE\tNOTE")
	for _, s := range skills {
		size, _ := s.Score("size")
		concise, _ := s.Score("conciseness")
		fmt.Fprintf(tw, "  %s\t%.0f\t%.0f\t%.0f\t%s\n",
			s.Skill, s.Overall, size, concise, s.Detail("size"))
	}
	_ = tw.Flush()
}

func init() {
	checksCmd.Flags().BoolVar(&checksFlags.NoMarketplace, "no-marketplace", false, "skip marketplace manifest validation")
	checksCmd.Flags().StringVar(&checksFlags.License, "license", "",
		"license every SKILL.md must declare; overrides checks.license (default: the field is forbidden)")
	checksCmd.Flags().BoolVar(&checksFlags.NoSignals, "no-signals", false, "skip the advisory skill-quality signals")
	runCmd.AddCommand(checksCmd)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
