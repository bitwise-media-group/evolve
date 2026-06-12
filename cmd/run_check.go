// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/run"
)

// CheckFlags holds the flags for `evolve run check`.
type CheckFlags struct {
	NoMarketplace bool
}

var checkFlags = CheckFlags{}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Run Tier 0 static checks: skill frontmatter, manifests, marketplace consistency",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		repo, err := opts.Repo()
		if err != nil {
			return err
		}
		cfg := opts.ChecksConfig()
		if checkFlags.NoMarketplace {
			cfg.Marketplace = false
		}

		findings, err := run.Checks(repo, cfg)
		if err != nil {
			return err
		}
		for _, f := range findings {
			fmt.Fprintf(cmd.ErrOrStderr(), "FAIL: %s\n", f.Message)
		}
		if n := len(findings); n > 0 {
			return failOrWarn(cmd, "check: %d %s", n, plural(n, "failure", "failures"))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "check: all checks passed (%s layout, %d %s)\n",
			repo.Kind, len(repo.Plugins), plural(len(repo.Plugins), "plugin", "plugins"))
		return nil
	},
}

func init() {
	checkCmd.Flags().BoolVar(&checkFlags.NoMarketplace, "no-marketplace", false, "skip marketplace manifest validation")
	runCmd.AddCommand(checkCmd)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
