// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/cli"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/run"
	"github.com/bitwise-media-group/evolve/internal/runner"
	"github.com/bitwise-media-group/evolve/internal/tokencount"
	"github.com/bitwise-media-group/evolve/internal/version"
)

// RunFlags holds the flags every `run` subcommand inherits from runCmd's
// persistent flag set.
type RunFlags struct {
	// Strict restores exit 1 when checks or evals fail; without it a failed
	// run prints a warning and exits 0.
	Strict bool
	// NoSandbox disables the OS filesystem sandbox that confines agent writes,
	// an escape hatch for hosts without the sandbox helper (config:
	// sandbox.enabled=false is the durable equivalent).
	NoSandbox bool
}

var runFlags = RunFlags{}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the eval tiers: static checks, trigger accuracy, behavioral evals",
}

// failOrWarn resolves a run that completed with failures: under --strict it
// returns an exit-1 error, otherwise it warns on stderr and the command
// exits 0.
func failOrWarn(cmd *cobra.Command, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if runFlags.Strict {
		return fmt.Errorf("%s: %w", msg, cli.ErrFailures)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "WARN: %s (pass --strict to exit 1)\n", msg)
	return nil
}

// SweepFlags holds the flags `run triggers` and `run evals` share.
type SweepFlags struct {
	Skill          string
	Models         string
	Timeout        int
	Jobs           int
	MaxTurns       int
	CountOnly      bool
	NewOnly        bool
	FailedOnly     bool
	ModifiedOnly   bool
	KeepWorkspaces bool
}

func (f *SweepFlags) register(cmd *cobra.Command, defaultTimeout int) {
	cmd.Flags().StringVar(&f.Skill, "skill", "", "only run evals for this skill")
	cmd.Flags().StringVar(&f.Models, "models", "",
		`comma-separated provider names / model ids, or "all" (default: config default_models or "anthropic")`)
	cmd.Flags().IntVar(&f.Timeout, "timeout", defaultTimeout, "seconds per agent run")
	cmd.Flags().IntVar(&f.Jobs, "jobs", provider.DefaultJobs(), "concurrent agent runs (default: ceil(cpus/2))")
	cmd.Flags().IntVar(&f.MaxTurns, "max-turns", provider.DefaultMaxTurns,
		"max agent turns per eval (config: max_turns; a per-eval max_turns overrides both)")
	cmd.Flags().BoolVar(&f.CountOnly, "count-only", false, "skip agent runs; only compute token usage per model")
	cmd.Flags().BoolVar(&f.NewOnly, "new", false,
		"only run evals whose stored results are missing values a rerun could fill")
	cmd.Flags().BoolVar(&f.FailedOnly, "failed", false,
		"only run evals that did not pass on a previous run (combine with --new to also rerun missing ones)")
	cmd.Flags().BoolVar(&f.ModifiedOnly, "modified", false,
		"only run evals whose authored skill content or case definition changed since their stored results")
	cmd.Flags().BoolVar(&f.KeepWorkspaces, "keep-workspaces", false, "keep throwaway workspaces for debugging")
	cmd.Flags().String("stale-results", "",
		"keep|drop stored results for models outside default_models (default: prompt on a terminal, else keep)")
}

// sweepOptions resolves the global flags and the sweep flags into the engine
// configuration triggers and evals share.
func (f *SweepFlags) sweepOptions(cmd *cobra.Command) (run.Options, error) {
	repo, err := opts.Repo()
	if err != nil {
		return run.Options{}, err
	}
	sandbox, err := resolveSandbox(repo, runFlags.NoSandbox)
	if err != nil {
		return run.Options{}, err
	}
	selected, err := opts.Selections(f.Models)
	if err != nil {
		return run.Options{}, err
	}
	counter, err := opts.Counter(cmd.ErrOrStderr())
	if err != nil {
		return run.Options{}, err
	}
	// Flag wins when set; otherwise fall back to the config's max_turns.
	maxTurns := f.MaxTurns
	if !cmd.Flags().Changed("max-turns") && opts.Viper != nil && opts.Viper.IsSet("max_turns") {
		maxTurns = opts.Viper.GetInt("max_turns")
	}
	return run.Options{
		Repo:           repo,
		Selected:       selected,
		Counter:        counter,
		Runner:         &runner.Exec{Sandbox: sandbox},
		HostSandboxed:  sandbox.Enabled,
		SkillFilter:    f.Skill,
		Timeout:        time.Duration(f.Timeout) * time.Second,
		Jobs:           f.Jobs,
		MaxTurns:       maxTurns,
		CountOnly:      f.CountOnly,
		New:            f.NewOnly,
		Failed:         f.FailedOnly,
		Modified:       f.ModifiedOnly,
		KeepWorkspaces: f.KeepWorkspaces,
		ResultsFormat:  opts.ResultsFormat,
		ToolVersion:    version.Version,
		Now:            time.Now,
		Stdout:         cmd.OutOrStdout(),
		Stderr:         cmd.ErrOrStderr(),
	}, nil
}

// perTierTimeouts resolves the triggers/evals timeouts for a combined sweep: the
// per-tier defaults (120s / 600s) unless the user set --timeout explicitly, in
// which case it applies to both tiers.
func perTierTimeouts(cmd *cobra.Command, timeout int) (trigger, eval time.Duration) {
	trigger = 120 * time.Second
	eval = 600 * time.Second
	if cmd.Flags().Changed("timeout") {
		trigger = time.Duration(timeout) * time.Second
		eval = trigger
	}
	return trigger, eval
}

func saveCounter(counter *tokencount.Counter) error {
	if err := counter.Save(); err != nil {
		return fmt.Errorf("saving token-count cache: %w", err)
	}
	return nil
}

func init() {
	runCmd.PersistentFlags().BoolVar(&runFlags.Strict, "strict", false,
		"exit 1 when checks or evals fail (default: warn and exit 0)")
	runCmd.PersistentFlags().BoolVar(&runFlags.NoSandbox, "no-sandbox", false,
		"disable the OS sandbox that confines agent writes to the workspace (config: sandbox.enabled)")
	runCmd.AddCommand(runAllCmd)
	rootCmd.AddCommand(runCmd)
}
