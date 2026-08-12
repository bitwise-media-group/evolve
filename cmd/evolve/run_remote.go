// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/remote"
	"github.com/bitwise-media-group/evolve/internal/run"
	"github.com/bitwise-media-group/evolve/internal/telemetry"
	"github.com/bitwise-media-group/evolve/internal/version"
)

// remoteSelections resolves (model, harness) selections without probing PATH
// for any CLI — the whole point of remote execution is that this machine
// need not carry one. Models come from the configured registry, harnesses
// from the configured roster (narrowed by --harness); the server's runner
// fleet is the actual eligibility gate, per unit, at admission.
func remoteSelections(f *SweepFlags) ([]harness.Selection, error) {
	if err := opts.ValidateFilterRestrictions(f.Harness, f.Models); err != nil {
		return nil, err
	}
	models, err := opts.ConfiguredModels()
	if err != nil {
		return nil, err
	}
	configured, err := opts.Harnesses()
	if err != nil {
		return nil, err
	}
	eligible := configured
	if len(f.Harness) > 0 {
		want := map[string]bool{}
		for _, id := range f.Harness {
			want[strings.TrimSpace(id)] = true
		}
		eligible = nil
		for _, h := range configured {
			if want[h.ID()] {
				eligible = append(eligible, h)
			}
		}
	}
	return harness.Select(opts.ModelsSpec(strings.Join(f.Models, ",")), models, eligible)
}

// remoteJudge resolves the judge model for remote evals: the model only —
// the pod binds it on each unit's own harness. Unresolvable degrades to no
// judge with a warning, mirroring the local default-judge behavior.
func remoteJudge(cmd *cobra.Command, f *SweepFlags) model.Model {
	token := f.judgeModel(cmd)
	models, err := opts.AvailableModels()
	if err != nil {
		return model.Model{}
	}
	for _, m := range models {
		if m.MatchedBy([]string{token}) {
			return m
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "WARN: judge model %q not found; llm assertions will error\n", token)
	return model.Model{}
}

// runRemote executes one tier sweep on the configured remote: plain output
// (the TUI is local-only for now), no sandbox, no token counting — the pod
// does the executing, this side plans, uploads, monitors, and lands results.
func runRemote(cmd *cobra.Command, f *SweepFlags, tiers plan.Tiers, runs int, evalFilter, failMsg string) error {
	if f.CountOnly {
		return errors.New("--count-only is a local operation; drop --remote")
	}
	url, err := requireRemoteURL(cmd)
	if err != nil {
		return err
	}
	repo, err := opts.Repo()
	if err != nil {
		return err
	}
	selected, err := remoteSelections(f)
	if err != nil {
		return err
	}
	store, err := remote.NewStore()
	if err != nil {
		return err
	}
	client, err := remote.NewClient(cmd.Context(), store, url)
	if err != nil {
		return err
	}

	stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	maxTurns := f.MaxTurns
	if !cmd.Flags().Changed("max-turns") && opts.Viper != nil && opts.Viper.IsSet("max_turns") {
		maxTurns = opts.Viper.GetInt("max_turns")
	}
	baseline := f.Baseline
	if !cmd.Flags().Changed("baseline") && opts.Viper != nil && opts.Viper.IsSet("baseline") {
		baseline = opts.Viper.GetBool("baseline")
	}
	triggerTO, evalTO := perTierTimeouts(cmd, f.Timeout)

	var judge model.Model
	if tiers.Evals {
		judge = remoteJudge(cmd, f)
	}

	failed, err := remote.Sweep(cmd.Context(), remote.SweepOptions{
		Options: run.Options{
			Repo:           repo,
			Selected:       selected,
			AssumeRunnable: true,
			PluginFilter:   f.Plugin,
			SkillFilter:    f.Skill,
			Timeout:        time.Duration(f.Timeout) * time.Second,
			Jobs:           f.Jobs,
			MaxTurns:       maxTurns,
			Baseline:       baseline,
			New:            f.NewOnly,
			Failed:         f.FailedOnly,
			Modified:       f.ModifiedOnly,
			ResultsFormat:  opts.ResultsFormat,
			ToolVersion:    version.Version,
			Now:            time.Now,
			Stdout:         stdout,
			Stderr:         stderr,
			Reporter:       telemetry.WrapReporter(run.NewPlainReporter(stdout, stderr)),
		},
		Client:         client,
		Tiers:          tiers,
		Runs:           runs,
		EvalFilter:     evalFilter,
		TriggerTimeout: triggerTO,
		EvalTimeout:    evalTO,
		Judge:          judge,
		ClientVersion:  version.Version,
	})
	if err != nil {
		return err
	}
	if err := opts.RegenerateReports(); err != nil {
		return err
	}
	if failed {
		return failOrWarn(cmd, "%s", failMsg)
	}
	return nil
}

func init() {
	runCmd.PersistentFlags().Bool("remote", false,
		"execute on the configured patchy remote-evaluation service (config: remote.default)")
	runCmd.PersistentFlags().Bool("local", false,
		"execute locally even when remote.default is set")
	runCmd.PersistentFlags().String("remote-url", "",
		"patchy remote-evaluation service URL (config: remote.url, env EVOLVE_REMOTE_URL)")
	runCmd.MarkFlagsMutuallyExclusive("remote", "local")
}
