// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/provider"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check each provider: runner CLI on PATH, credential set, counting API reachable",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		providers, _, err := opts.Providers()
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "PROVIDER\tCLI\tCREDENTIAL\tTOKEN COUNTING")
		for _, p := range providers {
			cliPath := "missing (" + p.CLI()[0] + ")"
			if path, ok := provider.ResolveCLI(p); ok {
				cliPath = path
			}

			credential := "not set (" + p.EnvKeys()[0] + ")"
			for _, env := range p.EnvKeys() {
				if os.Getenv(env) != "" {
					credential = env
					break
				}
			}

			counting := probeCounting(cmd.Context(), p)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name(), cliPath, credential, counting)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "\nCursor model ids are config-driven: run `agent models` for the live list and"+
			" pin them via providers.cursor.models in the .evolve config file.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

// probeCounting makes one tiny counting call for providers that support it.
func probeCounting(ctx context.Context, p provider.Provider) string {
	tc, ok := p.(provider.TokenCounter)
	if !ok {
		return "n/a (no counting API)"
	}
	hasCredential := false
	for _, env := range p.EnvKeys() {
		if os.Getenv(env) != "" {
			hasCredential = true
			break
		}
	}
	if !hasCredential {
		return "skipped (no credential)"
	}
	models := p.Models()
	if len(models) == 0 {
		return "skipped (no models)"
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tokens, err := tc.CountTokens(ctx, models[0].ID, "ping")
	if err != nil {
		return fmt.Sprintf("failed: %v", err)
	}
	return fmt.Sprintf("ok (%d tokens for %q on %s)", tokens, "ping", models[0].ID)
}
