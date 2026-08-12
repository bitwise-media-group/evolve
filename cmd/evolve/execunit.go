// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/cli"
	"github.com/bitwise-media-group/evolve/internal/grade"
	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/remote"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/run"
	"github.com/bitwise-media-group/evolve/internal/runner"
	"github.com/bitwise-media-group/evolve/internal/tokencount"
	"github.com/bitwise-media-group/evolve/internal/version"
)

// execUnitCmd is the hidden in-pod verb: patchy's evaluation Jobs run
// `evolve exec-unit` against a staged workspace bundle and unit spec,
// emitting the EVOLVE-EVENT stream on stdout. Exit codes: 0 = the unit
// completed (graded FAILs are not Job failures), 1 = execution error, 2 =
// bad spec or bundle.
var execUnitCmd = &cobra.Command{
	Use:    "exec-unit",
	Short:  "Execute one staged evaluation unit (in-pod; reads EVOLVE_UNIT_FILE / EVOLVE_BUNDLE_DIR).",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runExecUnit(cmd)
	},
}

func init() {
	rootCmd.AddCommand(execUnitCmd)
}

// execFatal emits a fatal event so the controller records why the pod could
// not produce a result, then returns the error for the exit code.
func execFatal(rep *remote.EventReporter, err error) error {
	rep.Emit(remote.Event{Type: remote.TypeFatal, Error: err.Error()})
	return err
}

func runExecUnit(cmd *cobra.Command) error {
	stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()

	unitFile := os.Getenv(remote.EnvUnitFile)
	bundleDir := os.Getenv(remote.EnvBundleDir)
	if unitFile == "" || bundleDir == "" {
		return fmt.Errorf("exec-unit: %s and %s are required", remote.EnvUnitFile, remote.EnvBundleDir)
	}
	raw, err := os.ReadFile(unitFile)
	if err != nil {
		return fmt.Errorf("exec-unit: read unit spec: %w", err)
	}
	var unit remote.UnitSpec
	if err := json.Unmarshal(raw, &unit); err != nil {
		return fmt.Errorf("exec-unit: parse unit spec: %w", err)
	}
	rep := remote.NewEventReporter(stdout, unit.Plugin)

	switch {
	case unit.Skill == "" || unit.Model == "":
		return execFatal(rep, fmt.Errorf("exec-unit: unit spec lacks skill or model"))
	case unit.Tier != 1 && unit.Tier != 2:
		return execFatal(rep, fmt.Errorf("exec-unit: tier %d is not 1 or 2", unit.Tier))
	}
	if fi, err := os.Stat(bundleDir); err != nil || !fi.IsDir() {
		return execFatal(rep, fmt.Errorf("exec-unit: bundle dir %s: %v", bundleDir, err))
	}

	m, ok := model.ModelByID(model.AllModels(nil), unit.Model)
	if !ok {
		return execFatal(rep, fmt.Errorf("exec-unit: model %q is not in the registry", unit.Model))
	}
	h, err := resolvePodHarness(unit, m)
	if err != nil {
		return execFatal(rep, err)
	}
	sel := harness.Selection{Model: m, Harness: h}

	// Bundles are plugin-relative: skills/ and evals/ under the root. The
	// Repo is constructed directly — the bundle carries no plugin manifest
	// for layout detection, deliberately.
	repo := &layout.Repo{
		Root: bundleDir,
		Kind: layout.Single,
		Plugins: []layout.Plugin{{
			Name:      unit.Plugin,
			Dir:       bundleDir,
			SkillsDir: filepath.Join(bundleDir, "skills"),
			EvalsDir:  filepath.Join(bundleDir, "evals"),
		}},
	}
	resultsDir := filepath.Join(bundleDir, "evals", unit.Skill)
	if err := seedPriorEntry(resultsDir, &unit, sel.Key()); err != nil {
		return execFatal(rep, err)
	}

	// The workspace initializer and agent sessions shell out to git; a bare
	// pod HOME has no identity, so supply a synthetic one.
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "evolve", "GIT_AUTHOR_EMAIL": "evolve@pod.invalid",
		"GIT_COMMITTER_NAME": "evolve", "GIT_COMMITTER_EMAIL": "evolve@pod.invalid",
	} {
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}

	exec := &runner.Exec{} // sandbox off: the pod IS the sandbox
	opts := run.Options{
		Repo:     repo,
		Selected: []harness.Selection{sel},
		// Token estimates are the submitting client's concern; the pod holds
		// only the agent credential, so counting is disabled outright.
		Counter:    tokencount.New(filepath.Join(os.TempDir(), "evolve-token-counts.json"), stderr),
		CounterFor: func(string) (model.TokenCounter, bool) { return nil, false },
		Runner:     exec,
		// The pod is the sandbox: evolve's own OS sandbox stays off, and
		// HostSandboxed tells the harnesses to disable the CLIs' sandboxes.
		HostSandboxed: true,
		// The runner image carries exactly one CLI, resolved before the pod
		// started; eligibility was the server's admission decision.
		AssumeRunnable: true,
		SkillFilter:    []string{unit.Skill},
		Jobs:           unit.Jobs,
		MaxTurns:       unit.MaxTurns,
		Baseline:       unit.Baseline,
		Filter:         casesFilter(&unit),
		ResultsFormat:  "json",
		ToolVersion:    version.Version,
		Now:            time.Now,
		Stdout:         stderr, // stdout is the event stream, exclusively
		Stderr:         stderr,
		Reporter:       rep,
	}

	triggerTimeout, evalTimeout := 120*time.Second, 600*time.Second
	if unit.TimeoutMS > 0 {
		d := time.Duration(unit.TimeoutMS) * time.Millisecond
		triggerTimeout, evalTimeout = d, d
	}

	sweep := run.SweepOptions{
		Options:        opts,
		Tiers:          plan.Tiers{Triggers: unit.Tier == 1, Evals: unit.Tier == 2},
		Runs:           unit.RunsPerQuery,
		TriggerTimeout: triggerTimeout,
		EvalTimeout:    evalTimeout,
	}
	if unit.Tier == 2 {
		sweep.Judge = podJudge(&unit, sel, exec, rep)
	}

	started := time.Now()
	failed, err := run.Sweep(cmd.Context(), sweep)
	if err != nil {
		// Execution errors exit 1 (via ErrFailures), unlike spec/bundle
		// errors above, which exit 2.
		return execFatal(rep, fmt.Errorf("%w: exec-unit: sweep: %v", cli.ErrFailures, err))
	}

	result, err := collectResult(resultsDir, &unit, sel, failed, time.Since(started))
	if err != nil {
		return execFatal(rep, fmt.Errorf("%w: %v", cli.ErrFailures, err))
	}
	rep.Emit(remote.Event{
		Type:   remote.TypeResult,
		Unit:   &remote.UnitRef{Plugin: unit.Plugin, Skill: unit.Skill, Key: sel.Key(), Kind: kindName(unit.Tier)},
		Result: result,
	})
	return nil
}

// kindName renders the tier as the wire kind.
func kindName(tier int) string {
	if tier == 1 {
		return "triggers"
	}
	return "evals"
}

// resolvePodHarness picks the unit's harness: the first preference whose CLI
// is actually on PATH — a runner image carries exactly one.
func resolvePodHarness(unit remote.UnitSpec, m model.Model) (harness.Harness, error) {
	prefs := make([]string, 0, len(unit.Harnesses))
	for _, opt := range unit.Harnesses {
		prefs = append(prefs, opt.Harness)
		h, ok := harness.ByID(opt.Harness)
		if !ok {
			continue
		}
		if _, found := harness.Available(h); !found {
			continue
		}
		if !m.Supports(h.ID()) {
			return nil, fmt.Errorf("exec-unit: model %s does not support harness %s", m.ID, h.ID())
		}
		return h, nil
	}
	return nil, fmt.Errorf("exec-unit: no harness among %v has its CLI in this image", prefs)
}

// seedPriorEntry lands the client's prior results entry into the bundle's
// (results-free) results file, so fingerprints, previous-run snapshots, and
// baselines behave exactly as they do locally.
func seedPriorEntry(resultsDir string, unit *remote.UnitSpec, key string) error {
	if len(unit.PriorEntry) == 0 {
		return nil
	}
	f, _, err := results.LoadDir(resultsDir, unit.Plugin, unit.Skill)
	if err != nil {
		return fmt.Errorf("exec-unit: load results: %w", err)
	}
	if unit.Tier == 1 {
		var entry results.TriggerEntry
		if err := json.Unmarshal(unit.PriorEntry, &entry); err != nil {
			return fmt.Errorf("exec-unit: parse prior trigger entry: %w", err)
		}
		f.SetTrigger(key, &entry)
	} else {
		var entry results.EvalEntry
		if err := json.Unmarshal(unit.PriorEntry, &entry); err != nil {
			return fmt.Errorf("exec-unit: parse prior eval entry: %w", err)
		}
		f.SetEval(key, &entry)
	}
	if _, err := f.SaveDir(resultsDir, "json"); err != nil {
		return fmt.Errorf("exec-unit: seed prior entry: %w", err)
	}
	return nil
}

// casesFilter renders the unit's case allowlist as a plan.Filter; nil runs
// everything.
func casesFilter(unit *remote.UnitSpec) *plan.Filter {
	if unit.Cases == nil {
		return nil
	}
	set := make(map[string]bool, len(unit.Cases))
	for _, c := range unit.Cases {
		set[c] = true
	}
	f := &plan.Filter{Skills: map[string]bool{unit.Skill: true}}
	if unit.Tier == 1 {
		f.Triggers = map[string]map[string]bool{unit.Skill: set}
	} else {
		f.Evals = map[string]map[string]bool{unit.Skill: set}
	}
	return f
}

// podJudge binds the judge on the unit's own harness (the v1 constraint the
// client validates before submitting); anything unresolvable degrades to an
// UnavailableJudge plus a warn event, never a dead pod.
func podJudge(unit *remote.UnitSpec, sel harness.Selection, exec run.Runner, rep *remote.EventReporter) grade.Judge {
	if unit.Judge == nil {
		return run.UnavailableJudge{Reason: "no judge model in the unit spec"}
	}
	jm, ok := model.ModelByID(model.AllModels(nil), unit.Judge.Model)
	if !ok {
		rep.Warn("judge model %s is not in the registry; llm assertions will error", unit.Judge.Model)
		return run.UnavailableJudge{Reason: "judge model " + unit.Judge.Model + " unknown"}
	}
	if !jm.Supports(sel.Harness.ID()) {
		rep.Warn("judge model %s cannot run on harness %s; llm assertions will error",
			unit.Judge.Model, sel.Harness.ID())
		return run.UnavailableJudge{Reason: "judge model " + unit.Judge.Model + " not runnable on " + sel.Harness.ID()}
	}
	judge, err := run.NewHarnessJudge(harness.Selection{Model: jm, Harness: sel.Harness}, exec, true)
	if err != nil {
		rep.Warn("judge unavailable: %v", err)
		return run.UnavailableJudge{Reason: err.Error()}
	}
	return judge
}

// collectResult loads the finished results file and renders the unit's
// result event payload.
func collectResult(resultsDir string, unit *remote.UnitSpec, sel harness.Selection,
	failed bool, elapsed time.Duration) (*remote.UnitResult, error) {
	f, _, err := results.LoadDir(resultsDir, unit.Plugin, unit.Skill)
	if err != nil {
		return nil, fmt.Errorf("exec-unit: reload results: %w", err)
	}

	result := &remote.UnitResult{
		Tier:    unit.Tier,
		Model:   unit.Model,
		Harness: sel.Harness.ID(),
		Failed:  failed,
	}
	sum := &result.Summary
	sum.Outcome = "ok"
	sum.ElapsedMS = elapsed.Milliseconds()

	if unit.Tier == 1 {
		entry := f.Trigger(sel.Key())
		if entry == nil {
			return nil, fmt.Errorf("exec-unit: no trigger entry for %s after the sweep", sel.Key())
		}
		sum.CasesPassed = intOr(entry.Summary.Passed)
		sum.CasesFailed = intOr(entry.Summary.Failed)
		for _, r := range entry.Results {
			appendCase(sum, r.Query, r.Passed)
		}
		result.Entry, err = remote.MarshalTriggerEntry(entry)
		return result, err
	}

	entry := f.Eval(sel.Key())
	if entry == nil {
		return nil, fmt.Errorf("exec-unit: no eval entry for %s after the sweep", sel.Key())
	}
	sum.CasesPassed = intOr(entry.Summary.Passed)
	sum.CasesFailed = intOr(entry.Summary.Failed)
	sum.CasesErrored = intOr(entry.Summary.Errored)
	if m := entry.Summary.Measured; m != nil {
		sum.TokenUsage = remote.TokenUsage{
			InputTokens:         int64(intOr(m.InputTokens)),
			OutputTokens:        int64(intOr(m.OutputTokens)),
			CacheReadTokens:     int64(intOr(m.CacheReadTokens)),
			CacheCreationTokens: int64(intOr(m.CacheCreationTokens)),
		}
		if m.CostUSD != nil {
			sum.TokenUsage.CostUSD = *m.CostUSD
		}
	}
	for _, r := range entry.Results {
		appendCase(sum, r.ID, r.Passed)
	}
	result.Entry, err = remote.MarshalEvalEntry(entry)
	return result, err
}

// appendCase adds one bounded case status (the CR caps the list at 256).
func appendCase(sum *remote.ResultSummary, id string, passed *bool) {
	if len(sum.Cases) >= 256 {
		return
	}
	if len(id) > 128 {
		id = id[:128]
	}
	sum.Cases = append(sum.Cases, remote.CaseStatus{ID: id, Passed: passed != nil && *passed})
}

// intOr unwraps an optional count.
func intOr(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
