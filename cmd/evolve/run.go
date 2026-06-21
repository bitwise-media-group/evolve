// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/bitwise-media-group/evolve/internal/cli"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/run"
	"github.com/bitwise-media-group/evolve/internal/runner"
	"github.com/bitwise-media-group/evolve/internal/telemetry"
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
	Plugin         []string
	Skill          []string
	Models         []string
	Timeout        int
	Jobs           int
	MaxTurns       int
	CountOnly      bool
	Baseline       bool
	NewOnly        bool
	FailedOnly     bool
	ModifiedOnly   bool
	KeepWorkspaces bool
	NoTUI          bool
}

func (f *SweepFlags) register(cmd *cobra.Command, defaultTimeout int) {
	cmd.Flags().StringSliceVar(&f.Plugin, "plugin", nil,
		"only run evals for these plugins (repeatable / comma-separated; alias: --plugins)")
	cmd.Flags().StringSliceVar(&f.Skill, "skill", nil,
		"only run evals for these skills (repeatable / comma-separated; alias: --skills)")
	cmd.Flags().StringSliceVar(&f.Models, "model", nil,
		`provider names / model ids, or "all" (repeatable / comma-separated; alias: --models; `+
			`default: config default_models or "anthropic")`)
	cmd.Flags().IntVar(&f.Timeout, "timeout", defaultTimeout, "seconds per agent run")
	cmd.Flags().IntVar(&f.Jobs, "jobs", provider.DefaultJobs(), "concurrent agent runs (default: ceil(cpus/2))")
	cmd.Flags().IntVar(&f.MaxTurns, "max-turns", provider.DefaultMaxTurns,
		"max agent turns per eval (config: max_turns; a per-eval max_turns overrides both)")
	cmd.Flags().BoolVar(&f.CountOnly, "count-only", false, "skip agent runs; only compute token usage per model")
	cmd.Flags().BoolVar(&f.Baseline, "baseline", true,
		"benchmark each eval without the skill (its lift), recomputed only when the eval or its fixtures "+
			"change (disable with --baseline=false; config: baseline)")
	cmd.Flags().BoolVar(&f.NewOnly, "new", false,
		"only run evals whose stored results are missing values a rerun could fill")
	cmd.Flags().BoolVar(&f.FailedOnly, "failed", false,
		"only run evals that did not pass on a previous run (combine with --new to also rerun missing ones)")
	cmd.Flags().BoolVar(&f.ModifiedOnly, "modified", false,
		"only run evals whose authored skill content or case definition changed since their stored results")
	cmd.Flags().BoolVar(&f.KeepWorkspaces, "keep-workspaces", false, "keep throwaway workspaces for debugging")
	cmd.Flags().String("stale-results", "",
		"keep|drop stored results for models outside default_models (default: prompt on a terminal, else keep)")
	cmd.Flags().BoolVar(&f.NoTUI, "no-tui", false,
		"disable the interactive TUI even on a terminal (also: EVOLVE_NO_TUI=1)")
	cmd.Flags().SetNormalizeFunc(sweepFlagAliases)
}

// sweepFlagAliases maps the plural flag aliases to their canonical singular
// names so --plugins/--skills/--models behave identically to
// --plugin/--skill/--model (they share the same backing slice).
func sweepFlagAliases(_ *pflag.FlagSet, name string) pflag.NormalizedName {
	switch name {
	case "plugins":
		name = "plugin"
	case "skills":
		name = "skill"
	case "models":
		name = "model"
	}
	return pflag.NormalizedName(name)
}

// sweepOptions resolves the global flags and the sweep flags into the engine
// configuration triggers and evals share.
func (f *SweepFlags) sweepOptions(cmd *cobra.Command) (run.Options, error) {
	return f.sweepOptionsW(cmd, cmd.ErrOrStderr())
}

// sweepOptionsW is sweepOptions with an explicit destination for token-counter
// diagnostics, so the TUI can redirect them off the terminal.
func (f *SweepFlags) sweepOptionsW(cmd *cobra.Command, counterOut io.Writer) (run.Options, error) {
	repo, err := opts.Repo()
	if err != nil {
		return run.Options{}, err
	}
	sandbox, err := resolveSandbox(repo, runFlags.NoSandbox)
	if err != nil {
		return run.Options{}, err
	}
	selected, err := opts.Selections(strings.Join(f.Models, ","))
	if err != nil {
		return run.Options{}, err
	}
	counter, err := opts.Counter(counterOut)
	if err != nil {
		return run.Options{}, err
	}
	// Flag wins when set; otherwise fall back to the config's max_turns.
	maxTurns := f.MaxTurns
	if !cmd.Flags().Changed("max-turns") && opts.Viper != nil && opts.Viper.IsSet("max_turns") {
		maxTurns = opts.Viper.GetInt("max_turns")
	}
	// Baseline defaults on; config baseline=false disables it when the flag is unset.
	baseline := f.Baseline
	if !cmd.Flags().Changed("baseline") && opts.Viper != nil && opts.Viper.IsSet("baseline") {
		baseline = opts.Viper.GetBool("baseline")
	}
	stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	return run.Options{
		Repo:           repo,
		Selected:       selected,
		Counter:        counter,
		Runner:         &runner.Exec{Sandbox: sandbox},
		HostSandboxed:  sandbox.Enabled,
		PluginFilter:   f.Plugin,
		SkillFilter:    f.Skill,
		Timeout:        time.Duration(f.Timeout) * time.Second,
		Jobs:           f.Jobs,
		MaxTurns:       maxTurns,
		CountOnly:      f.CountOnly,
		Baseline:       baseline,
		New:            f.NewOnly,
		Failed:         f.FailedOnly,
		Modified:       f.ModifiedOnly,
		KeepWorkspaces: f.KeepWorkspaces,
		ResultsFormat:  opts.ResultsFormat,
		ToolVersion:    version.Version,
		Now:            time.Now,
		Stdout:         stdout,
		Stderr:         stderr,
		// When telemetry is on, the decorator records metrics and structured logs
		// from the reporter's events; when off it returns the PlainReporter
		// unchanged, so plain-mode output is byte-for-byte what it was. The TUI
		// path overrides Reporter in uiRun with its own wrapped reporter.
		Reporter: telemetry.WrapReporter(run.PlainReporter{Stdout: stdout, Stderr: stderr}),
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
