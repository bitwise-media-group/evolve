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

	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/model"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check each harness (CLI on PATH, credential) and each vendor's counting API",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := opts.ChecksConfig().ValidateSignals(); err != nil {
			return fmt.Errorf("config: %w", err)
		}
		harnesses, err := opts.Harnesses()
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "HARNESS\tCLI\tCREDENTIAL")
		for _, h := range harnesses {
			cliPath := "missing (" + h.CLI()[0] + ")"
			if path, ok := harness.Available(h); ok {
				cliPath = path
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", h.ID(), cliPath, credentialStatus(h.EnvKeys()))
		}
		if err := w.Flush(); err != nil {
			return err
		}

		w2 := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
		fmt.Fprintln(w2, "\nPROVIDER\tTOKEN COUNTING")
		for _, p := range model.Providers() {
			fmt.Fprintf(w2, "%s\t%s\n", p.ID, probeCounting(cmd.Context(), p.ID))
		}
		if err := w2.Flush(); err != nil {
			return err
		}

		warnings, err := opts.UnsupportedModelWarnings()
		if err != nil {
			return err
		}
		for _, msg := range warnings {
			fmt.Fprintf(cmd.OutOrStdout(), "WARN: %s\n", msg)
		}

		fmt.Fprintln(cmd.OutOrStdout(), "\nCursor model ids are config-driven: run `agent models` for the live list and"+
			" pin them via providers.cursor.models in the .evolve config file.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

// credentialStatus reports the first set credential env var, or that none of
// them is set (naming the highest-precedence one).
func credentialStatus(envKeys []string) string {
	if len(envKeys) == 0 {
		return "n/a"
	}
	for _, env := range envKeys {
		if os.Getenv(env) != "" {
			return env
		}
	}
	return "not set (" + envKeys[0] + ")"
}

// hasCredential reports whether any of the env vars is set.
func hasCredential(envKeys []string) bool {
	for _, env := range envKeys {
		if os.Getenv(env) != "" {
			return true
		}
	}
	return false
}

// probeCounting makes one tiny counting call for a vendor that supports it.
func probeCounting(ctx context.Context, providerID string) string {
	tc, ok := model.CounterFor(providerID)
	if !ok {
		return "n/a (no counting API)"
	}
	if !hasCredential(model.CounterEnvKeys(providerID)) {
		return "skipped (no credential)"
	}
	bareID := ""
	for _, m := range model.AllModels(nil) {
		if m.ProviderID == providerID {
			bareID = m.BareID()
			break
		}
	}
	if bareID == "" {
		return "skipped (no models)"
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tokens, err := tc.CountTokens(ctx, bareID, "ping")
	if err != nil {
		return fmt.Sprintf("failed: %v", err)
	}
	return fmt.Sprintf("ok (%d tokens for %q on %s)", tokens, "ping", bareID)
}
