// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the build version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "evolve %s (commit %s, built %s)\n",
			version.Version, version.Commit, version.BuildDate)
		return err
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
