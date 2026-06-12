// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestRunAllForwardsFlags drives the real commands: flags set on `run all`
// reach the tiers that define them, and everything left unset keeps each
// tier's own default — most notably the per-tier timeouts.
func TestRunAllForwardsFlags(t *testing.T) {
	if err := runAllCmd.Flags().Parse([]string{
		"--models", "claude-haiku-4-5", "--runs", "1", "--jobs", "1",
	}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, sub := range []*cobra.Command{checkCmd, triggersCmd, casesCmd, reportCmd} {
		if err := forwardFlags(runAllCmd.Flags(), sub.Flags()); err != nil {
			t.Fatalf("forward to %s: %v", sub.Name(), err)
		}
	}

	if triggersFlags.Models != "claude-haiku-4-5" || triggersFlags.Jobs != 1 || triggersFlags.Runs != 1 {
		t.Errorf("triggers flags = %q/%d/%d, want claude-haiku-4-5/1/1",
			triggersFlags.Models, triggersFlags.Jobs, triggersFlags.Runs)
	}
	if casesFlags.Models != "claude-haiku-4-5" || casesFlags.Jobs != 1 {
		t.Errorf("cases flags = %q/%d, want claude-haiku-4-5/1", casesFlags.Models, casesFlags.Jobs)
	}
	if triggersFlags.Timeout != 120 || casesFlags.Timeout != 600 {
		t.Errorf("timeouts = %d/%d, want the per-tier defaults 120/600",
			triggersFlags.Timeout, casesFlags.Timeout)
	}
}
