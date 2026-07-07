// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

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

// TestSweepFlagsMultiValue covers --plugin/--skill/--model: each accepts a
// comma-separated list, repeats to append, and resolves its plural alias
// (--plugins/--skills/--models) to the same backing slice.
func TestSweepFlagsMultiValue(t *testing.T) {
	tests := []struct {
		name                  string
		args                  []string
		plugin, skill, models []string
	}{
		{
			name:   "comma-separated",
			args:   []string{"--plugin", "a,b", "--skill", "x,y", "--model", "m1,m2"},
			plugin: []string{"a", "b"}, skill: []string{"x", "y"}, models: []string{"m1", "m2"},
		},
		{
			name:   "repeated flags append",
			args:   []string{"--plugin", "a", "--plugin", "b", "--skill", "x", "--skill", "y"},
			plugin: []string{"a", "b"}, skill: []string{"x", "y"},
		},
		{
			name:   "plural aliases",
			args:   []string{"--plugins", "a,b", "--skills", "x", "--models", "m1"},
			plugin: []string{"a", "b"}, skill: []string{"x"}, models: []string{"m1"},
		},
		{
			name:   "comma and repeat mixed",
			args:   []string{"--model", "m1,m2", "--model", "m3"},
			models: []string{"m1", "m2", "m3"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var f SweepFlags
			cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
			f.register(cmd, 120)
			if err := cmd.Flags().Parse(tc.args); err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !slices.Equal(f.Plugin, tc.plugin) {
				t.Errorf("Plugin = %q, want %q", f.Plugin, tc.plugin)
			}
			if !slices.Equal(f.Skill, tc.skill) {
				t.Errorf("Skill = %q, want %q", f.Skill, tc.skill)
			}
			if !slices.Equal(f.Models, tc.models) {
				t.Errorf("Models = %q, want %q", f.Models, tc.models)
			}
		})
	}
}

// TestWriteCommandsEnforceVersionPin pins the wiring: every command that
// rewrites results or reports consults the version pin before doing anything
// else. An invalid constraint errors on any binary (a valid one is skipped for
// the test binary's non-semver "dev" version), so it proves each RunE calls
// CheckVersionPin and propagates its error.
func TestWriteCommandsEnforceVersionPin(t *testing.T) {
	saved := opts.Viper
	t.Cleanup(func() { opts.Viper = saved })
	opts.Viper = viper.New()
	opts.Viper.Set("version", "banana")

	for name, runE := range map[string]func(*cobra.Command, []string) error{
		"run triggers": triggersCmd.RunE,
		"run evals":    evalsCmd.RunE,
		"run all":      runAllCmd.RunE,
		"report":       reportCmd.RunE,
	} {
		cmd := &cobra.Command{}
		cmd.SetErr(&bytes.Buffer{})
		err := runE(cmd, nil)
		if err == nil || !strings.Contains(err.Error(), "invalid version constraint") {
			t.Errorf("%s: err = %v, want the version-pin error before any work", name, err)
		}
	}
}
