// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/grade"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/workspace"
)

// EvalOptions configures an eval sweep.
type EvalOptions struct {
	Options
	EvalFilter string
	JudgeModel string
}

// Evals executes the sweep. failed reports whether any executed eval failed.
func Evals(ctx context.Context, opts EvalOptions) (failed bool, err error) {
	sets, err := opts.Repo.EvalSets()
	if err != nil {
		return false, err
	}
	for _, set := range sets {
		if set.EvalsPath == "" || (opts.SkillFilter != "" && set.Skill != opts.SkillFilter) {
			continue
		}
		setFailed, err := runEvalSet(ctx, opts, set)
		failed = failed || setFailed
		if err != nil {
			return failed, err
		}
	}
	return failed, nil
}

func runEvalSet(ctx context.Context, opts EvalOptions, set layout.EvalSet) (failed bool, err error) {
	spec, err := evalspec.LoadEvals(set.EvalsPath)
	if err != nil {
		return false, err
	}
	warnSkillName(&opts.Options, set, set.EvalsPath, spec.SkillName)
	allEvals := filterEvals(spec.Evals, opts.EvalFilter)
	if len(allEvals) == 0 {
		return false, nil
	}
	skillMD, err := os.ReadFile(filepath.Join(set.SkillDir, "SKILL.md"))
	if err != nil {
		return false, fmt.Errorf("reading skill under test: %w", err)
	}
	rep := opts.reporter()
	file, reset := results.LoadDir(set.ResultsDir, set.Plugin.Name, set.Skill)
	if reset {
		rep.Warn("  warn: %s has an old or unreadable results schema; starting fresh (schema %d)\n",
			opts.Repo.Rel(results.Find(set.ResultsDir)), results.Schema)
	}

	for _, sel := range opts.Selected {
		unitFailed, err := runEvalUnit(ctx, opts, set, sel, file, skillMD, allEvals)
		failed = failed || unitFailed
		if err != nil {
			return failed, err
		}
	}
	return failed, nil
}

// runEvalUnit runs one (skill, model) eval unit against the shared results file
// and skill payload. allEvals is the skill's eval list after any --eval narrowing;
// per-provider skips and the selection filter are applied here. A unit with no
// applicable evals reports nothing and returns cleanly.
func runEvalUnit(ctx context.Context, opts EvalOptions, set layout.EvalSet, sel provider.Selection,
	file *results.File, skillMD []byte, allEvals []evalspec.Eval,
) (failed bool, err error) {
	rep := opts.reporter()
	evalRunner, isEvalRunner := sel.Provider.(provider.EvalRunner)
	cli, cliFound := provider.ResolveCLI(sel.Provider)
	execute := isEvalRunner && cliFound && !opts.CountOnly

	evals := applicableEvals(allEvals, sel.Provider.Name(), set.Skill, opts.Filter)
	if len(evals) == 0 {
		return false, nil
	}
	ref := UnitRef{Skill: set.Skill, Key: sel.Key(), Kind: KindEvals}

	probe := func(c evalspec.Eval) bool {
		return opts.Counter.Count(ctx, sel.Provider, sel.Model.ID, payload(skillMD, c.Prompt)) != nil
	}
	_, countCapable := sel.Provider.(provider.TokenCounter)
	if opts.New {
		reportsUsage := isEvalRunner && evalRunner.ReportsUsage()
		if reason := evalSkipReason(
			file.Evals[sel.Key()], evals, sel.Model, execute, reportsUsage, countCapable, probe,
		); reason != "" {
			rep.UnitSkipped(ref, reason)
			return false, nil
		}
	}
	if !execute && !opts.CountOnly {
		rep.Warn("  warn: no behavioral runner for %s; token counts only\n", sel.Key())
	}

	mode := ModeCountOnly
	if execute {
		mode = ModeRun
	}
	rep.UnitStarted(ref, len(evals), 0, mode)

	// Token counting stays on this goroutine; only eval runs go parallel,
	// each in its own workspace.
	entryResults := make([]results.EvalResult, len(evals))
	for i, c := range evals {
		tokens := opts.Counter.Count(ctx, sel.Provider, sel.Model.ID, payload(skillMD, c.Prompt))
		entryResults[i] = results.EvalResult{
			ID:       c.ID,
			Estimate: results.NewEstimate(tokens, sel.Model.InputUSD),
		}
	}
	if execute {
		batchFailed, err := runEvals(ctx, opts, set, sel, ref, evalRunner, cli, evals, entryResults)
		failed = failed || batchFailed
		if err != nil {
			return failed, err
		}
	}

	entry := buildEvalEntry(opts, sel, execute, entryResults)
	file.SetEval(sel.Key(), entry)
	saved, err := file.SaveDir(set.ResultsDir, opts.ResultsFormat)
	if err != nil {
		return failed, err
	}
	sum := UnitSummary{Executed: entry.Executed, Total: entry.Summary.Total}
	if entry.Executed {
		sum.Passed = *entry.Summary.Passed
		sum.AvgRunSeconds = entry.Summary.AvgRunSeconds
		if entry.Summary.Errored != nil {
			sum.Errored = *entry.Summary.Errored
		}
	}
	rep.UnitFinished(ref, sum, opts.Repo.Rel(saved))
	return failed, nil
}

// evalOutcome is the tri-state result of one eval run: it passed, it ran and
// failed assertions, or the agent run itself failed (a runtime error) and
// never produced a gradable answer.
type evalOutcome int

const (
	outcomePass evalOutcome = iota
	outcomeFail
	outcomeError
)

func runEvals(ctx context.Context, opts EvalOptions, set layout.EvalSet, sel provider.Selection, ref UnitRef,
	evalRunner provider.EvalRunner, cli string, evals []evalspec.Eval, entryResults []results.EvalResult,
) (bool, error) {
	var failedAny bool
	g, runCtx := errgroup.WithContext(ctx)
	g.SetLimit(opts.Jobs)
	outcomes := make([]evalOutcome, len(evals))
	for i, c := range evals {
		g.Go(func() error {
			outcome, result, err := runEval(runCtx, opts, set, sel, ref, evalRunner, cli, c, i)
			if err != nil {
				return err
			}
			result.Estimate = entryResults[i].Estimate // counting happened up front
			entryResults[i] = result
			outcomes[i] = outcome
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return false, err
	}
	// A runtime error and an assertion failure both fail the sweep, but a
	// runtime error is not an abort: it is recorded per eval so a blocked
	// credential surfaces as "N errored", not a cascade of cancellations.
	for _, o := range outcomes {
		if o != outcomePass {
			failedAny = true
		}
	}
	return failedAny, nil
}

func runEval(ctx context.Context, opts EvalOptions, set layout.EvalSet, sel provider.Selection, ref UnitRef,
	evalRunner provider.EvalRunner, cli string, c evalspec.Eval, index int,
) (evalOutcome, results.EvalResult, error) {
	rep := opts.reporter()
	rep.ItemStarted(ref, ItemStart{Index: index, Label: c.ID, Runs: 1})
	copies := make(map[string]string, len(c.Files))
	for _, f := range c.Files {
		copies[f.Dest] = f.Source
	}
	// Evals isolate the skill under test: only it is symlinked in, keeping the
	// agent's per-turn context lean (no sibling-skill descriptions to re-read).
	parent, keep := opts.retain()
	ws, cleanup, err := workspace.New(parent, "evals.", []string{set.SkillDir},
		unionSkillDirs(opts.Selected), copies, keep)
	if err != nil {
		return outcomeError, results.EvalResult{}, err
	}
	defer cleanup()
	if opts.KeepWorkspaces {
		rep.Warn("  workspace kept (%s): %s\n", c.ID, ws)
	}

	timeout := opts.Timeout
	if c.TimeoutSeconds > 0 {
		timeout = time.Duration(c.TimeoutSeconds) * time.Second
	}
	// Precedence: a per-eval max_turns wins; otherwise the run-level setting
	// (CLI flag or config); otherwise the provider's DefaultMaxTurns (0 here).
	maxTurns := c.MaxTurns
	if maxTurns == 0 {
		maxTurns = opts.MaxTurns
	}
	spec := evalRunner.EvalSpec(ws, provider.EvalInput{
		Prompt:       c.Prompt,
		MaxTurns:     maxTurns,
		AllowedTools: c.AllowedTools,
	}, sel.Model.ID)
	spec.Argv[0] = cli

	res, err := opts.Runner.Run(ctx, spec, timeout, nil)
	if err != nil {
		return outcomeError, results.EvalResult{}, err
	}
	if res.TimedOut {
		rep.Warn("  warn: %s timed out after %s; grading partial output\n", cli, timeout)
	}

	// When the run retains workspaces, keep this one and write its full output
	// log so the live TUI can open both; empty otherwise (ws is about to go).
	workdir, logPath := retainArtifacts(parent, ws, res.Stdout)

	// A run that produced no usable output (auth blocked, crash) is a runtime
	// failure, not an eval failure — surface it loudly instead of grading empty
	// output into a silent FAIL.
	runSeconds := results.Round1(res.Elapsed.Seconds()) // agent run only; grading excluded
	if reason := evalRunner.RuntimeError(res.Stdout, res.ExitCode, res.TimedOut); reason != "" {
		rep.ItemDone(ref, ItemResult{
			Index:  index,
			Label:  c.ID,
			Status: StatusError,
			Detail: fmt.Sprintf("  [ERROR] %s: %s runtime failure (exit %d): %s\n",
				c.ID, cli, res.ExitCode, errorDetail(reason, res.StderrTail)),
			Metrics:       ItemMetrics{AvgRunSeconds: &runSeconds},
			WorkspacePath: workdir,
			LogPath:       logPath,
		})
		return outcomeError, results.EvalResult{
			ID:           c.ID,
			Name:         c.Name,
			RuntimeError: reason,
			Timing:       &results.Timing{ExecutorDurationSeconds: &runSeconds},
		}, nil
	}

	output, usage := evalRunner.ParseEvalOutput(res.Stdout)

	// Grade assertions; buffer the verdict lines so concurrent evals don't
	// interleave their output.
	graded := make([]results.GradedAssertion, len(c.Assertions))
	var lines strings.Builder
	evalPassed := true
	for i, a := range c.Assertions {
		passed, evidence := grade.Assertion(ctx, a, grade.Options{
			Runner:         opts.Runner,
			Workspace:      ws,
			Output:         output,
			ExpectedOutput: c.ExpectedOutput,
			Timeout:        timeout,
			JudgeModel:     opts.JudgeModel,
		})
		source := "assertion"
		if a.FromExpectation {
			source = "expectation"
		}
		graded[i] = results.GradedAssertion{
			Assertion: a,
			Text:      grade.Describe(a),
			Passed:    passed,
			Evidence:  evidence,
			Source:    source,
		}
		if passed != nil && !*passed {
			evalPassed = false
		}
		mark := "SKIP"
		if passed != nil {
			mark = "FAIL"
			if *passed {
				mark = "PASS"
			}
		}
		fmt.Fprintf(&lines, "  [%s] %s: %s\n", mark, c.ID, graded[i].Text)
	}
	status := StatusFail
	if evalPassed {
		status = StatusPass
	}

	result := results.EvalResult{
		ID:           c.ID,
		Name:         c.Name,
		Passed:       &evalPassed,
		Expectations: graded,
		Summary:      results.SummarizeExpectations(graded),
		Timing:       &results.Timing{ExecutorDurationSeconds: &runSeconds},
		Measured:     measured(sel.Model, usage),
	}
	if result.Measured != nil {
		result.Timing.TotalTokens = result.Measured.TotalTokens()
	}
	rep.ItemDone(ref, ItemResult{
		Index: index, Label: c.ID, Status: status, Detail: lines.String(),
		Output:        headLines(output, evalOutputLines),
		Metrics:       evalItemMetrics(&runSeconds, result.Measured, result.Summary),
		WorkspacePath: workdir,
		LogPath:       logPath,
	})
	outcome := outcomeFail
	if evalPassed {
		outcome = outcomePass
	}
	return outcome, result, nil
}

// evalItemMetrics lifts a finished eval's measured usage and assertion tally
// into the per-case metric pointers the dashboard renders.
func evalItemMetrics(dur *float64, m *results.Measured, s *results.GradeSummary) ItemMetrics {
	im := ItemMetrics{AvgRunSeconds: dur}
	if m != nil {
		im.InputTokens = m.InputTokens
		im.CacheReadTokens = m.CacheReadTokens
		im.CacheCreationTokens = m.CacheCreationTokens
		im.OutputTokens = m.OutputTokens
		im.CostUSD = m.CostUSD
	}
	if s != nil {
		p, tot := s.Passed, s.Total
		im.AssertPassed = &p
		im.AssertTotal = &tot
	}
	return im
}

// evalOutputLines bounds the agent text the live TUI keeps in memory (one copy
// per eval for the whole run); the full output is written to the run log file,
// which the dashboard opens on demand.
const evalOutputLines = 20

// headLines keeps the first n lines of s, marking truncation with an ellipsis
// line so the pane shows there is more in the full log.
func headLines(s string, n int) string {
	parts := strings.SplitN(s, "\n", n+1)
	if len(parts) > n {
		return strings.Join(parts[:n], "\n") + "\n…"
	}
	return s
}

// retainArtifacts records a finished run's workspace and writes its full output
// log when the run retains workspaces (parent set); both are empty otherwise, so
// the TUI shows no open hint for a workspace that is about to be removed. The log
// sits beside the workspace under the run-scoped root, removed with it.
func retainArtifacts(parent, ws string, stdout []byte) (workdir, logPath string) {
	if parent == "" {
		return "", ""
	}
	logPath = ws + ".log"
	if err := os.WriteFile(logPath, stdout, 0o644); err != nil {
		logPath = ""
	}
	return ws, logPath
}

// errorDetail renders a runtime failure's diagnostic from the two places a CLI
// can report one: reason is the provider's read of stdout (claude puts its
// error subtype and `errors` array there, never on stderr), and stderrTail is
// what the CLI wrote to stderr (where most other runners report). Whichever is
// present is shown — and both when they are — so an error is never reduced to a
// bare exit code. The runner already caps StderrTail to the last few KB; the
// fatal message sits at the end, so keep the tail and collapse newlines.
func errorDetail(reason, stderrTail string) string {
	tail := strings.ReplaceAll(strings.TrimSpace(stderrTail), "\n", " ")
	const max = 200
	if len(tail) > max {
		tail = "…" + tail[len(tail)-max:]
	}
	switch {
	case reason != "" && tail != "":
		return reason + "; stderr: " + tail
	case reason != "":
		return reason
	case tail != "":
		return tail
	default:
		return "(no error output)"
	}
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
		InputTokens:         usage.InputTokens,
		CacheReadTokens:     usage.CacheReadTokens,
		CacheCreationTokens: usage.CacheCreationTokens,
		OutputTokens:        usage.OutputTokens,
		CostUSD:             cost,
	}
}

func buildEvalEntry(opts EvalOptions, sel provider.Selection, executed bool,
	entryResults []results.EvalResult,
) *results.EvalEntry {
	entry := &results.EvalEntry{
		Header:  opts.header(sel, executed),
		Results: entryResults,
		Summary: results.EvalSummary{Total: len(entryResults)},
	}

	estimates := make([]*results.Estimate, len(entryResults))
	for i, r := range entryResults {
		estimates[i] = r.Estimate
	}
	entry.Summary.Estimate = results.SumEstimates(estimates)

	if executed {
		passed, failed, errored := 0, 0, 0
		var runSum float64
		var runCount int
		for _, r := range entryResults {
			switch {
			case r.RuntimeError != "":
				errored++
			case r.Passed != nil && *r.Passed:
				passed++
			case r.Passed != nil:
				failed++
			}
			if r.Timing != nil && r.Timing.ExecutorDurationSeconds != nil {
				runSum += *r.Timing.ExecutorDurationSeconds
				runCount++
			}
		}
		entry.Summary.Passed = &passed
		entry.Summary.Failed = &failed
		if errored > 0 {
			entry.Summary.Errored = &errored
		}
		if passed+failed > 0 {
			rate := results.Round6(float64(passed) / float64(passed+failed))
			entry.Summary.PassRate = &rate
		}
		if runCount > 0 {
			avg := results.Round1(runSum / float64(runCount))
			entry.Summary.AvgRunSeconds = &avg
		}
		entry.Summary.Measured = sumMeasured(entryResults)
	}
	return entry
}

func sumMeasured(entryResults []results.EvalResult) *results.Measured {
	var in, cacheRead, cacheCreation, out int
	var cost float64
	var hasIn, hasCacheRead, hasCacheCreation, hasOut, hasCost bool
	for _, r := range entryResults {
		if r.Measured == nil {
			continue
		}
		if r.Measured.InputTokens != nil {
			in += *r.Measured.InputTokens
			hasIn = true
		}
		if r.Measured.CacheReadTokens != nil {
			cacheRead += *r.Measured.CacheReadTokens
			hasCacheRead = true
		}
		if r.Measured.CacheCreationTokens != nil {
			cacheCreation += *r.Measured.CacheCreationTokens
			hasCacheCreation = true
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
	if !hasIn && !hasCacheRead && !hasCacheCreation && !hasOut && !hasCost {
		return nil
	}
	sum := &results.Measured{}
	if hasIn {
		sum.InputTokens = &in
	}
	if hasCacheRead {
		sum.CacheReadTokens = &cacheRead
	}
	if hasCacheCreation {
		sum.CacheCreationTokens = &cacheCreation
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

// evalSkipReason is why --new may skip this skill/model, or "" when a (re)run
// is needed. Fields a run could never fill are exempt: costs for unpriced
// models, execution fields when no runner is available or this invocation is
// count-only, measured usage for providers that never report it (cursor),
// and token counts the counting API cannot produce.
func evalSkipReason(entry *results.EvalEntry, evals []evalspec.Eval, model provider.Model,
	execute, reportsUsage, countCapable bool, probe func(evalspec.Eval) bool,
) string {
	stored := map[string]results.EvalResult{}
	if entry != nil {
		for _, r := range entry.Results {
			stored[r.ID] = r
		}
	}
	priced := model.InputUSD != nil && model.OutputUSD != nil
	var uncounted *evalspec.Eval
	for _, c := range evals {
		r, ok := stored[c.ID]
		if !ok {
			return ""
		}
		if execute && r.RuntimeError != "" {
			return "" // a prior runtime error is not a completed result; re-run it
		}
		if execute {
			if r.Passed == nil || r.Timing == nil || r.Timing.ExecutorDurationSeconds == nil {
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

func applicableEvals(evals []evalspec.Eval, providerName, skill string, f *Filter) []evalspec.Eval {
	if !f.skillIncluded(skill) {
		return nil
	}
	var out []evalspec.Eval
	for _, c := range evals {
		if c.SkipsProvider(providerName) {
			continue
		}
		if !f.evalIncluded(skill, c.ID) {
			continue
		}
		out = append(out, c)
	}
	return out
}
