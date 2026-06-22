// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TestRunAllForwardsFlags drives the real commands: flags set on `run all`
// reach the tiers that define them, and everything left unset keeps each
// tier's own default — most notably the per-tier timeouts.
func TestRunAllForwardsFlags(t *testing.T) {
	// runAllCmd is a package-level command, and a pflag StringSlice appends on
	// every Parse — so repeated runs (go test -count>1) would otherwise see
	// --model grow to [haiku, haiku, …]. Clear the slice flags so each run
	// starts from the flag defaults.
	runAllCmd.Flags().VisitAll(func(f *pflag.Flag) {
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		}
	})

	if err := runAllCmd.Flags().Parse([]string{
		"--models", "claude-haiku-4-5", "--runs", "1", "--jobs", "1",
	}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, sub := range []*cobra.Command{checksCmd, triggersCmd, evalsCmd, reportCmd} {
		if err := forwardFlags(runAllCmd.Flags(), sub.Flags()); err != nil {
			t.Fatalf("forward to %s: %v", sub.Name(), err)
		}
	}

	if !slices.Equal(triggersFlags.Models, []string{"claude-haiku-4-5"}) ||
		triggersFlags.Jobs != 1 || triggersFlags.Runs != 1 {
		t.Errorf("triggers flags = %q/%d/%d, want [claude-haiku-4-5]/1/1",
			triggersFlags.Models, triggersFlags.Jobs, triggersFlags.Runs)
	}
	if !slices.Equal(evalsFlags.Models, []string{"claude-haiku-4-5"}) || evalsFlags.Jobs != 1 {
		t.Errorf("evals flags = %q/%d, want [claude-haiku-4-5]/1", evalsFlags.Models, evalsFlags.Jobs)
	}
	if triggersFlags.Timeout != 120 || evalsFlags.Timeout != 600 {
		t.Errorf("timeouts = %d/%d, want the per-tier defaults 120/600",
			triggersFlags.Timeout, evalsFlags.Timeout)
	}
}
