// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/run"
)

// ChecksFlags holds the flags for `evolve run checks`.
type ChecksFlags struct {
	NoMarketplace bool
	License       string
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
		if n := len(findings); n > 0 {
			return failOrWarn(cmd, "checks: %d %s", n, plural(n, "failure", "failures"))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "checks: all checks passed (%s layout, %d %s)\n",
			repo.Kind, len(repo.Plugins), plural(len(repo.Plugins), "plugin", "plugins"))
		return nil
	},
}

func init() {
	checksCmd.Flags().BoolVar(&checksFlags.NoMarketplace, "no-marketplace", false, "skip marketplace manifest validation")
	checksCmd.Flags().StringVar(&checksFlags.License, "license", "",
		"license every SKILL.md must declare; overrides checks.license (default: the field is forbidden)")
	runCmd.AddCommand(checksCmd)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
