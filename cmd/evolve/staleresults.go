// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/cli"
)

// reconcileStaleResults runs at the start of the run and report commands. When
// the `models` restriction is configured and results files hold models outside
// it, it keeps or drops that data per --stale-results / the stale_results config,
// prompting on an interactive terminal and defaulting to keep (with a warning)
// when it cannot prompt. It is a no-op when no restriction is configured or no
// stale data exists.
func reconcileStaleResults(cmd *cobra.Command, interactive bool) error {
	active, configured, err := opts.ActiveModelKeys()
	if err != nil {
		return err
	}
	if !configured {
		return nil
	}
	// Validate the requested mode up front so a bad value fails fast, even when
	// there happens to be no stale data this run.
	mode := opts.StaleResultsMode(cmd)
	switch mode {
	case "", "keep", "drop":
	default:
		return fmt.Errorf("--stale-results: want keep or drop, got %q", mode)
	}
	stale, err := opts.FindStaleResults(active)
	if err != nil {
		return err
	}
	if len(stale) == 0 {
		return nil
	}
	models := cli.StaleModels(stale)

	if mode == "" {
		if interactive {
			mode = promptStaleResults(cmd, models)
		} else {
			mode = "keep"
		}
	}

	out := cmd.ErrOrStderr()
	if mode == "drop" {
		if err := opts.DropStaleResults(stale); err != nil {
			return err
		}
		fmt.Fprintf(out, "stale-results: dropped %d model(s) outside the configured models: %s\n",
			len(models), strings.Join(models, ", "))
		return nil
	}
	fmt.Fprintf(out, "stale-results: kept %d model(s) outside the configured models on disk "+
		"(excluded from reports); pass --stale-results=drop to prune: %s\n",
		len(models), strings.Join(models, ", "))
	return nil
}

// promptStaleResults asks the user whether to keep or drop stale results.
func promptStaleResults(cmd *cobra.Command, models []string) string {
	out := cmd.ErrOrStderr()
	fmt.Fprintf(out, "stale-results: results files hold %d model(s) outside the configured models:\n  %s\n",
		len(models), strings.Join(models, ", "))
	fmt.Fprint(out, "Keep them on disk or drop them? [keep/drop] (default keep): ")
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if s := strings.TrimSpace(strings.ToLower(line)); s == "drop" || s == "d" {
		return "drop"
	}
	return "keep"
}
