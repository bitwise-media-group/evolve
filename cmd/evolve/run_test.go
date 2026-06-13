// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/cli"
)

// TestFailOrWarn covers the two outcomes of a run that completed with
// failures: by default it warns on stderr and returns nil (exit 0), under
// --strict it returns cli.ErrFailures (exit 1).
func TestFailOrWarn(t *testing.T) {
	t.Cleanup(func() { runFlags.Strict = false })

	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&stderr)

	runFlags.Strict = false
	if err := failOrWarn(cmd, "evals: %d failed", 2); err != nil {
		t.Errorf("default: err = %v, want nil", err)
	}
	if got := stderr.String(); !strings.Contains(got, "WARN: evals: 2 failed") {
		t.Errorf("default: stderr = %q, want a WARN line", got)
	}

	stderr.Reset()
	runFlags.Strict = true
	if err := failOrWarn(cmd, "evals: %d failed", 2); !errors.Is(err, cli.ErrFailures) {
		t.Errorf("strict: err = %v, want cli.ErrFailures", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("strict: stderr = %q, want empty", stderr.String())
	}
}
