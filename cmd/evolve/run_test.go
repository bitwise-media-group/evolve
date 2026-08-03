// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/bitwise-media-group/evolve/internal/cli"
	"github.com/bitwise-media-group/evolve/internal/grade"
	"github.com/bitwise-media-group/evolve/internal/run"
	"github.com/bitwise-media-group/evolve/internal/runner"
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

// TestJudgeModelPrecedence pins the resolution order for the LLM judge:
// --judge-model wins when the user set it, otherwise the config's judge_model,
// otherwise the pinned default. Verdicts are only comparable across runs that
// used the same judge, so a silently wrong precedence here would re-base every
// stored grade.
func TestJudgeModelPrecedence(t *testing.T) {
	tests := []struct {
		name   string
		config any // nil leaves judge_model unset
		args   []string
		want   string
	}{
		{name: "default when unset", want: grade.DefaultJudgeModel},
		{name: "config wins over default", config: "claude-opus-5", want: "claude-opus-5"},
		{
			name: "flag wins over config", config: "claude-opus-5",
			args: []string{"--judge-model", "claude-haiku-4-5"}, want: "claude-haiku-4-5",
		},
		{
			name: "flag wins over default",
			args: []string{"--judge-model", "claude-opus-5"}, want: "claude-opus-5",
		},
		{name: "empty config falls back to default", config: "", want: grade.DefaultJudgeModel},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved := opts.Viper
			t.Cleanup(func() { opts.Viper = saved })
			opts.Viper = viper.New()
			if tc.config != nil {
				opts.Viper.Set("judge_model", tc.config)
			}

			var f SweepFlags
			cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
			f.register(cmd, 600)
			if err := cmd.Flags().Parse(tc.args); err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := f.judgeModel(cmd); got != tc.want {
				t.Errorf("judgeModel() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveJudge pins the judge failure surface: an explicitly named judge
// model (flag or config) that cannot run aborts at command start, while an
// unresolvable default degrades to a warned UnavailableJudge so repos without
// llm assertions keep running.
func TestResolveJudge(t *testing.T) {
	newCmd := func(t *testing.T, args ...string) (*SweepFlags, *cobra.Command) {
		t.Helper()
		var f SweepFlags
		cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
		f.register(cmd, 600)
		if err := cmd.Flags().Parse(args); err != nil {
			t.Fatalf("parse: %v", err)
		}
		return &f, cmd
	}
	common := run.Options{Runner: &runner.Exec{}}
	saved := opts.Viper
	t.Cleanup(func() { opts.Viper = saved })

	t.Run("default resolves", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)
		opts.Viper = viper.New()
		f, cmd := newCmd(t)
		var warn bytes.Buffer
		j, err := f.resolveJudge(cmd, common, &warn)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := j.(*run.HarnessJudge); !ok {
			t.Errorf("judge = %T, want *run.HarnessJudge", j)
		}
		if warn.Len() != 0 {
			t.Errorf("warn = %q, want none", warn.String())
		}
	})

	t.Run("unresolvable default degrades", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		opts.Viper = viper.New()
		f, cmd := newCmd(t)
		var warn bytes.Buffer
		j, err := f.resolveJudge(cmd, common, &warn)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := j.(run.UnavailableJudge); !ok {
			t.Errorf("judge = %T, want run.UnavailableJudge", j)
		}
		if !strings.Contains(warn.String(), "WARN") || !strings.Contains(warn.String(), "llm assertions will fail") {
			t.Errorf("warn = %q, want a WARN line", warn.String())
		}
	})

	t.Run("explicit flag errors", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		opts.Viper = viper.New()
		f, cmd := newCmd(t, "--judge-model", "claude-sonnet-5")
		if _, err := f.resolveJudge(cmd, common, &bytes.Buffer{}); err == nil {
			t.Error("err = nil, want command-start error for an explicit judge model")
		}
	})

	t.Run("explicit config errors", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		opts.Viper = viper.New()
		opts.Viper.Set("judge_model", "claude-sonnet-5")
		f, cmd := newCmd(t)
		if _, err := f.resolveJudge(cmd, common, &bytes.Buffer{}); err == nil {
			t.Error("err = nil, want command-start error for a configured judge model")
		}
	})
}

// TestJudgeModelNilViper covers the no-config-file case: Options.Viper is nil
// before any config is loaded, and the resolver must not panic on it.
func TestJudgeModelNilViper(t *testing.T) {
	saved := opts.Viper
	t.Cleanup(func() { opts.Viper = saved })
	opts.Viper = nil

	var f SweepFlags
	cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
	f.register(cmd, 600)
	if got := f.judgeModel(cmd); got != grade.DefaultJudgeModel {
		t.Errorf("judgeModel() = %q, want %q", got, grade.DefaultJudgeModel)
	}
}
