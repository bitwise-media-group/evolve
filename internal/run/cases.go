// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/grade"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/workspace"
)

// CaseOptions configures a case sweep.
type CaseOptions struct {
	Options
	CaseFilter string
	JudgeModel string
}

// Cases executes the sweep. failed reports whether any executed case failed.
func Cases(ctx context.Context, opts CaseOptions) (failed bool, err error) {
	sets, err := opts.Repo.EvalSets()
	if err != nil {
		return false, err
	}
	for _, set := range sets {
		if set.CasesPath == "" || (opts.SkillFilter != "" && set.Skill != opts.SkillFilter) {
			continue
		}
		setFailed, err := runCaseSet(ctx, opts, set)
		failed = failed || setFailed
		if err != nil {
			return failed, err
		}
	}
	return failed, nil
}

func runCaseSet(ctx context.Context, opts CaseOptions, set layout.EvalSet) (failed bool, err error) {
	allCases, err := evalspec.LoadCases(set.CasesPath)
	if err != nil {
		return false, err
	}
	if opts.CaseFilter != "" {
		var filtered []evalspec.Case
		for _, c := range allCases {
			if c.ID == opts.CaseFilter {
				filtered = append(filtered, c)
			}
		}
		allCases = filtered
	}
	if len(allCases) == 0 {
		return false, nil
	}
	skillMD, err := os.ReadFile(filepath.Join(set.SkillDir, "SKILL.md"))
	if err != nil {
		return false, fmt.Errorf("reading skill under test: %w", err)
	}
	file := results.Load(set.ResultsPath, set.Plugin.Name, set.Skill)

	for _, sel := range opts.Selected {
		caseRunner, isCaseRunner := sel.Provider.(provider.CaseRunner)
		cli, cliFound := provider.ResolveCLI(sel.Provider)
		execute := isCaseRunner && cliFound && !opts.CountOnly

		cases := applicableCases(allCases, sel.Provider.Name())
		if len(cases) == 0 {
			continue
		}

		probe := func(c evalspec.Case) bool {
			return opts.Counter.Count(ctx, sel.Provider, sel.Model.ID, payload(skillMD, c.Prompt)) != nil
		}
		_, countCapable := sel.Provider.(provider.TokenCounter)
		if opts.New {
			reportsUsage := isCaseRunner && caseRunner.ReportsUsage()
			if reason := caseSkipReason(
				file.Cases[sel.Key()], cases, sel.Model, execute, reportsUsage, countCapable, probe,
			); reason != "" {
				fmt.Fprintf(opts.Stdout, "\n=== %s / %s (skip: %s) ===\n", set.Skill, sel.Key(), reason)
				continue
			}
		}
		if !execute && !opts.CountOnly {
			fmt.Fprintf(opts.Stderr, "  warn: no behavioral runner for %s; token counts only\n", sel.Key())
		}

		mode := "count-only"
		if execute {
			mode = "run"
		}
		fmt.Fprintf(opts.Stdout, "\n=== %s / %s (%s) ===\n", set.Skill, sel.Key(), mode)

		// Token counting stays on this goroutine; only case runs go parallel,
		// each in its own workspace.
		entryResults := make([]results.CaseResult, len(cases))
		for i, c := range cases {
			tokens := opts.Counter.Count(ctx, sel.Provider, sel.Model.ID, payload(skillMD, c.Prompt))
			entryResults[i] = results.CaseResult{
				ID:       c.ID,
				Estimate: results.NewEstimate(tokens, sel.Model.InputUSD),
			}
		}
		if execute {
			batchFailed, err := runCases(ctx, opts, set, sel, caseRunner, cli, cases, entryResults)
			failed = failed || batchFailed
			if err != nil {
				return failed, err
			}
		}

		entry := buildCaseEntry(opts, sel, execute, entryResults)
		file.SetCase(sel.Key(), entry)
		if err := file.Save(set.ResultsPath); err != nil {
			return failed, err
		}
		if entry.Executed {
			fmt.Fprintf(opts.Stdout, "  %d/%d cases passed%s\n",
				*entry.Summary.Passed, entry.Summary.Total, avgSuffix(entry.Summary.AvgRunSeconds))
		}
		fmt.Fprintf(opts.Stdout, "  -> %s\n", opts.Repo.Rel(set.ResultsPath))
	}
	return failed, nil
}

func runCases(ctx context.Context, opts CaseOptions, set layout.EvalSet, sel provider.Selection,
	caseRunner provider.CaseRunner, cli string, cases []evalspec.Case, entryResults []results.CaseResult) (bool, error) {

	var failedAny bool
	g, runCtx := errgroup.WithContext(ctx)
	g.SetLimit(opts.Jobs)
	verdicts := make([]bool, len(cases))
	for i, c := range cases {
		g.Go(func() error {
			passed, result, err := runCase(runCtx, opts, set, sel, caseRunner, cli, c)
			if err != nil {
				return err
			}
			result.Estimate = entryResults[i].Estimate // counting happened up front
			entryResults[i] = result
			verdicts[i] = passed
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return false, err
	}
	for _, ok := range verdicts {
		failedAny = failedAny || !ok
	}
	return failedAny, nil
}

func runCase(ctx context.Context, opts CaseOptions, set layout.EvalSet, sel provider.Selection,
	caseRunner provider.CaseRunner, cli string, c evalspec.Case) (bool, results.CaseResult, error) {

	ws, cleanup, err := workspace.New("cases.", set.Plugin.SkillsDir,
		unionSkillDirs(opts.Selected), c.Files, opts.KeepWorkspaces)
	if err != nil {
		return false, results.CaseResult{}, err
	}
	defer cleanup()
	if opts.KeepWorkspaces {
		fmt.Fprintf(opts.Stderr, "  workspace kept (%s): %s\n", c.ID, ws)
	}

	timeout := opts.Timeout
	if c.TimeoutSeconds > 0 {
		timeout = time.Duration(c.TimeoutSeconds) * time.Second
	}
	spec := caseRunner.CaseSpec(ws, provider.CaseInput{
		Prompt:       c.Prompt,
		MaxTurns:     c.MaxTurns,
		AllowedTools: c.AllowedTools,
	}, sel.Model.ID)
	spec.Argv[0] = cli

	res, err := opts.Runner.Run(ctx, spec, timeout, nil)
	if err != nil {
		return false, results.CaseResult{}, err
	}
	if res.TimedOut {
		fmt.Fprintf(opts.Stderr, "  warn: %s timed out after %s; grading partial output\n", cli, timeout)
	}
	output, usage := caseRunner.ParseCaseOutput(res.Stdout)
	runSeconds := results.Round1(res.Elapsed.Seconds()) // agent run only; grading excluded

	// Grade assertions; buffer the verdict lines so concurrent cases don't
	// interleave their output.
	graded := make([]results.GradedAssertion, len(c.Assertions))
	lines := ""
	casePassed := true
	for i, a := range c.Assertions {
		passed, evidence := grade.Assertion(ctx, a, grade.Options{
			Runner:     opts.Runner,
			Workspace:  ws,
			Output:     output,
			Timeout:    timeout,
			JudgeModel: opts.JudgeModel,
		})
		graded[i] = results.GradedAssertion{Assertion: a, Passed: passed, Evidence: evidence}
		if passed != nil && !*passed {
			casePassed = false
		}
		marker := "SKIP"
		if passed != nil {
			marker = "FAIL"
			if *passed {
				marker = "PASS"
			}
		}
		lines += fmt.Sprintf("  [%s] %s: %s\n", marker, c.ID, assertionLabel(a))
	}
	fmt.Fprint(opts.Stdout, lines)

	result := results.CaseResult{
		ID:         c.ID,
		Passed:     &casePassed,
		RunSeconds: &runSeconds,
		Assertions: graded,
		Measured:   measured(sel.Model, usage),
	}
	return casePassed, result, nil
}

// measured converts harness-reported usage, computing the cost from the
// model's pricing when the CLI does not report one (codex).
func measured(model provider.Model, usage *provider.Usage) *results.Measured {
	if usage == nil {
		return nil
	}
	cost := usage.CostUSD
	if cost == nil {
		cost = provider.UsageCostUSD(model, *usage)
	}
	if cost != nil {
		rounded := results.Round6(*cost)
		cost = &rounded
	}
	return &results.Measured{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CostUSD:      cost,
	}
}

func buildCaseEntry(opts CaseOptions, sel provider.Selection, executed bool,
	entryResults []results.CaseResult) *results.CaseEntry {
	entry := &results.CaseEntry{
		Header:  opts.header(sel, executed),
		Results: entryResults,
		Summary: results.CaseSummary{Total: len(entryResults)},
	}

	estimates := make([]*results.Estimate, len(entryResults))
	for i, r := range entryResults {
		estimates[i] = r.Estimate
	}
	entry.Summary.Estimate = results.SumEstimates(estimates)

	if executed {
		passed := 0
		var runSum float64
		var runCount int
		for _, r := range entryResults {
			if r.Passed != nil && *r.Passed {
				passed++
			}
			if r.RunSeconds != nil {
				runSum += *r.RunSeconds
				runCount++
			}
		}
		entry.Summary.Passed = &passed
		if runCount > 0 {
			avg := results.Round1(runSum / float64(runCount))
			entry.Summary.AvgRunSeconds = &avg
		}
		entry.Summary.Measured = sumMeasured(entryResults)
	}
	return entry
}

func sumMeasured(entryResults []results.CaseResult) *results.Measured {
	var in, out int
	var cost float64
	var hasIn, hasOut, hasCost bool
	for _, r := range entryResults {
		if r.Measured == nil {
			continue
		}
		if r.Measured.InputTokens != nil {
			in += *r.Measured.InputTokens
			hasIn = true
		}
		if r.Measured.OutputTokens != nil {
			out += *r.Measured.OutputTokens
			hasOut = true
		}
		if r.Measured.CostUSD != nil {
			cost += *r.Measured.CostUSD
			hasCost = true
		}
	}
	if !hasIn && !hasOut && !hasCost {
		return nil
	}
	sum := &results.Measured{}
	if hasIn {
		sum.InputTokens = &in
	}
	if hasOut {
		sum.OutputTokens = &out
	}
	if hasCost {
		rounded := results.Round6(cost)
		sum.CostUSD = &rounded
	}
	return sum
}

// caseSkipReason is why --new may skip this skill/model, or "" when a (re)run
// is needed. Fields a run could never fill are exempt: costs for unpriced
// models, execution fields when no runner is available or this invocation is
// count-only, measured usage for providers that never report it (cursor),
// and token counts the counting API cannot produce.
func caseSkipReason(entry *results.CaseEntry, cases []evalspec.Case, model provider.Model,
	execute, reportsUsage, countCapable bool, probe func(evalspec.Case) bool) string {

	stored := map[string]results.CaseResult{}
	if entry != nil {
		for _, r := range entry.Results {
			stored[r.ID] = r
		}
	}
	priced := model.InputUSD != nil && model.OutputUSD != nil
	var uncounted *evalspec.Case
	for _, c := range cases {
		r, ok := stored[c.ID]
		if !ok {
			return ""
		}
		if execute {
			if r.Passed == nil || r.RunSeconds == nil {
				return ""
			}
			if reportsUsage {
				if r.Measured == nil || r.Measured.InputTokens == nil || r.Measured.OutputTokens == nil {
					return ""
				}
				if priced && r.Measured.CostUSD == nil {
					return ""
				}
			}
		}
		// Estimates a provider can never produce (no counting API) are exempt.
		missingCount := countCapable && (r.Estimate == nil ||
			(model.InputUSD != nil && r.Estimate.InputCostUSD == nil))
		if uncounted == nil && missingCount {
			uncounted = &c
		}
	}
	if uncounted == nil {
		return "results complete"
	}
	if probe(*uncounted) {
		return ""
	}
	return "token counts unavailable"
}

func applicableCases(cases []evalspec.Case, providerName string) []evalspec.Case {
	var out []evalspec.Case
	for _, c := range cases {
		if !c.SkipsProvider(providerName) {
			out = append(out, c)
		}
	}
	return out
}

func assertionLabel(a evalspec.Assertion) string {
	for _, label := range []string{a.Text, a.Pattern, a.Run, a.Path} {
		if label != "" {
			return label
		}
	}
	return a.Type
}
