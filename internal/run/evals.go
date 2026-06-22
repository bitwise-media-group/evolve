// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/grade"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/plan"
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
	ctx, span := tracer().Start(ctx, "evolve.sweep", trace.WithAttributes(
		attribute.String("command", "evals"),
		attribute.Bool("count_only", opts.CountOnly),
		attribute.Int("jobs", opts.Jobs),
		attribute.Int("model_count", len(opts.Selected)),
	))
	defer func() { endSpan(span, err) }()
	opts.ensureReporter()

	sets, err := opts.Repo.EvalSets()
	if err != nil {
		return false, err
	}
	for _, set := range sets {
		if set.EvalsPath == "" || !opts.selects(set.Plugin.Name, set.Skill) {
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
	ctx, span := tracer().Start(ctx, "evolve.skill_set", trace.WithAttributes(
		attribute.String("plugin", set.Plugin.Name),
		attribute.String("skill", set.Skill),
	))
	defer func() { endSpan(span, err) }()

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
	// A behavioral eval depends on the whole skill the agent sees, so the entire
	// skill directory is the content fingerprint persisted for --modified.
	contentHash, err := skillContentHash(set.SkillDir)
	if err != nil {
		return false, fmt.Errorf("fingerprinting skill under test: %w", err)
	}
	rep := opts.reporter()
	file, reset := results.LoadDir(set.ResultsDir, set.Plugin.Name, set.Skill)
	if reset {
		rep.Warn("  warn: %s has an old or unreadable results schema; starting fresh (schema %d)\n",
			opts.Repo.Rel(results.Find(set.ResultsDir)), results.Schema)
	}

	for _, sel := range opts.Selected {
		unitFailed, err := runEvalUnit(ctx, opts, set, sel, file, skillMD, contentHash, allEvals)
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
	file *results.File, skillMD []byte, contentHash string, allEvals []evalspec.Eval,
) (failed bool, err error) {
	rep := opts.reporter()
	provName := sel.Provider.Name()
	evalRunner, _ := sel.Provider.(provider.EvalRunner)
	cli, _ := provider.ResolveCLI(sel.Provider)
	execute, reportsUsage, priced := evalCapabilities(opts.Options, sel)

	// modelApplicable is every eval valid for this model (skip_providers + skill
	// only), ignoring the selection filter, so a partial rerun preserves the evals
	// it does not touch. evals then narrows by the selection filter.
	modelApplicable := plan.ApplicableEvals(allEvals, provName, set.Skill, nil)
	evals := plan.ApplicableEvals(allEvals, provName, set.Skill, opts.Filter)
	if len(evals) == 0 {
		return false, nil
	}
	ref := plan.UnitRef{Skill: set.Skill, Key: sel.Key(), Kind: plan.KindEvals}
	ctx, span := tracer().Start(ctx, "evolve.unit", trace.WithAttributes(unitSpanAttrs(ref)...))
	defer func() { endSpan(span, err) }()

	// Per-case run-set: under --new/--failed/--modified keep only the evals with a
	// gap — the same predicate the TUI form preselects on, so CLI and TUI run an
	// identical set. A missing/stale baseline (--baseline) is itself a gap, so it
	// pulls its eval into the run-set additively: --new and --baseline compose
	// rather than one overriding the other, and a recomputed baseline always gets a
	// contemporaneous with-skill result to compare against.
	runSet := evals
	if opts.New || opts.Failed || opts.Modified {
		runSet = evalRunSet(file, sel, contentHash, evals, execute, reportsUsage, priced, opts)
		if len(runSet) == 0 {
			rep.UnitSkipped(ref, "results complete")
			return false, nil
		}
	}
	if !execute && !opts.CountOnly {
		rep.Warn("  warn: no behavioral runner for %s; token counts only\n", sel.Key())
	}

	mode := plan.ModeCountOnly
	if execute {
		mode = plan.ModeRun
	}
	rep.UnitStarted(ref, len(runSet), 0, mode)

	// Token counting fans out over opts.Jobs: on a cache miss each count is a
	// network round-trip, so a sequential loop stalls the unit before any eval
	// run starts (see Options.countTokens).
	texts := make([]string, len(runSet))
	for i, c := range runSet {
		texts[i] = payload(skillMD, c.Prompt)
	}
	tokens := opts.countTokens(ctx, sel, texts)
	entryResults := make([]results.EvalResult, len(runSet))
	for i, c := range runSet {
		entryResults[i] = results.EvalResult{
			ID:       c.ID,
			Estimate: results.NewEstimate(tokens[i], sel.Model.InputUSD),
			SpecHash: evalFingerprint(c),
		}
	}
	// The entry being replaced is the run we rotate into Previous and the source of
	// the baseline we carry forward. Capture it before the run (read-only) so each
	// eval can tell whether its baseline is stale, and before SetEval overwrites it.
	old := file.Evals[sel.Key()]
	var priorBaseline *results.EvalSnapshot
	if old != nil {
		priorBaseline = old.Baseline
	}

	// Each eval runs its without-skill baseline (when missing or stale) interleaved
	// right before its own run, rather than as a separate post-pass — so a stale
	// baseline streams as a visible "baseline running" row instead of a silent stall
	// at the end of the unit. freshBaseline collects the baselines that re-ran.
	var freshBaseline map[string]results.EvalCaseMetrics
	if execute {
		batchFailed, fresh, err := runEvals(ctx, opts, set, sel, ref, evalRunner, cli, runSet, entryResults, priorBaseline)
		failed = failed || batchFailed
		if err != nil {
			return failed, err
		}
		freshBaseline = fresh
	}

	merged := mergeEvalResults(old, entryResults, modelApplicable)
	entry := buildEvalEntry(opts, sel, execute, contentHash, merged, old, freshBaseline)
	file.SetEval(sel.Key(), entry)
	saved, err := file.SaveDir(set.ResultsDir, opts.ResultsFormat)
	if err != nil {
		return failed, err
	}
	sum := evalUnitSummary(entry)
	span.SetAttributes(
		attribute.Bool("executed", sum.Executed),
		attribute.Int("passed", sum.Passed),
		attribute.Int("errored", sum.Errored),
		attribute.Int("total", sum.Total),
	)
	rep.UnitFinished(ref, sum, opts.Repo.Rel(saved))
	return failed, nil
}

// evalUnitSummary lifts a finished entry's rollup into the reporter's UnitSummary.
func evalUnitSummary(entry *results.EvalEntry) UnitSummary {
	sum := UnitSummary{Executed: entry.Executed, Total: entry.Summary.Total}
	if entry.Executed {
		sum.Passed = *entry.Summary.Passed
		sum.AvgRunSeconds = entry.Summary.AvgRunSeconds
		if entry.Summary.Errored != nil {
			sum.Errored = *entry.Summary.Errored
		}
	}
	return sum
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

// runEvals runs every run-set eval concurrently (Jobs at a time). Within each
// eval's slot the without-skill baseline runs first — when --baseline is on and
// the stored baseline is missing or stale — immediately followed by the with-skill
// run, so a recomputed baseline streams as a visible "baseline running" row right
// before its own run rather than as a silent post-pass at the end of the unit.
// priorBaseline is the entry's stored baseline (the staleness reference); the
// returned map holds the baselines that re-ran this round, keyed by eval id.
func runEvals(ctx context.Context, opts EvalOptions, set layout.EvalSet, sel provider.Selection, ref plan.UnitRef,
	evalRunner provider.EvalRunner, cli string, evals []evalspec.Eval, entryResults []results.EvalResult,
	priorBaseline *results.EvalSnapshot,
) (bool, map[string]results.EvalCaseMetrics, error) {
	var failedAny bool
	g, runCtx := errgroup.WithContext(ctx)
	g.SetLimit(opts.Jobs)
	outcomes := make([]evalOutcome, len(evals))
	baselines := make([]*results.EvalCaseMetrics, len(evals)) // nil unless a baseline re-ran
	for i, c := range evals {
		g.Go(func() error {
			// Baseline first (interleaved), when on and stale, so the row shows its
			// without-skill run before its with-skill run.
			if opts.Baseline {
				fp := evalFingerprint(c)
				if baselineStale(priorBaseline, c.ID, fp) {
					_, base, err := runEval(runCtx, opts, sel, ref, evalRunner, cli, c, i, nil, true)
					if err != nil {
						return err
					}
					m := results.EvalCaseMetricsOf(base)
					m.Fingerprint = fp
					baselines[i] = &m
				}
			}
			outcome, result, err := runEval(runCtx, opts, sel, ref, evalRunner, cli, c, i, []string{set.SkillDir}, false)
			if err != nil {
				return err
			}
			result.Estimate = entryResults[i].Estimate // counting happened up front
			result.SpecHash = entryResults[i].SpecHash // fingerprint computed up front
			entryResults[i] = result
			outcomes[i] = outcome
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return false, nil, err
	}
	// A runtime error and an assertion failure both fail the sweep, but a
	// runtime error is not an abort: it is recorded per eval so a blocked
	// credential surfaces as "N errored", not a cascade of cancellations.
	for _, o := range outcomes {
		if o != outcomePass {
			failedAny = true
		}
	}
	var fresh map[string]results.EvalCaseMetrics
	for i, m := range baselines {
		if m == nil {
			continue
		}
		if fresh == nil {
			fresh = map[string]results.EvalCaseMetrics{}
		}
		fresh[evals[i].ID] = *m
	}
	return failedAny, fresh, nil
}

// evalRunSet narrows the applicable evals to those a --new/--failed/--modified
// sweep should rerun with the skill present: the same per-case predicate the TUI
// form preselects on, so CLI and TUI run an identical set. A missing/stale
// baseline is its own gap, so --baseline adds the eval here too — composing with
// --new rather than being overridden by it.
func evalRunSet(file *results.File, sel provider.Selection, contentHash string, evals []evalspec.Eval,
	execute, reportsUsage, priced bool, opts EvalOptions,
) []evalspec.Eval {
	var runSet []evalspec.Eval
	for _, c := range evals {
		r, storedContent, ok := lookupEval(file, sel.Key(), c.ID)
		fp := fingerprints{storedContent: storedContent, freshContent: contentHash, freshSpec: evalFingerprint(c)}
		reason := evalCaseReason(r, ok, execute, reportsUsage, priced, opts.New, opts.Failed, opts.Modified, fp)
		if reason != ReasonNone || evalBaselineNeeded(file, sel.Key(), c, execute, opts.Baseline) {
			runSet = append(runSet, c)
		}
	}
	return runSet
}

// evalBaselineNeeded reports whether eval c needs a baseline run for this model:
// --baseline is on, the model executes, and the stored baseline is missing or
// stale. It is the shared gap the engine run-set and the TUI form both OR into
// their selection, so the two stay in parity.
func evalBaselineNeeded(file *results.File, key string, c evalspec.Eval, execute, wantBaseline bool) bool {
	if !wantBaseline || !execute {
		return false
	}
	var prior *results.EvalSnapshot
	if e := file.Evals[key]; e != nil {
		prior = e.Baseline
	}
	return baselineStale(prior, c.ID, evalFingerprint(c))
}

// baselineStale reports whether eval id needs its baseline (re)computed: missing
// from the prior snapshot, or recorded against a different eval fingerprint.
func baselineStale(prior *results.EvalSnapshot, id, fp string) bool {
	if prior == nil {
		return true
	}
	m, ok := prior.Cases[id]
	if !ok {
		return true
	}
	return m.Fingerprint != fp
}

// mergeBaseline assembles the new entry's baseline snapshot: carry the prior
// baseline forward, overlay the cases that re-ran this round, drop cases no longer
// in the spec, and re-summarize. RanAt advances only when a case actually re-ran.
// Returns nil when no baseline data remains.
func mergeBaseline(old *results.EvalSnapshot, fresh map[string]results.EvalCaseMetrics,
	current []results.EvalResult, now string,
) *results.EvalSnapshot {
	cases := map[string]results.EvalCaseMetrics{}
	if old != nil {
		maps.Copy(cases, old.Cases)
	}
	maps.Copy(cases, fresh)
	keep := make(map[string]bool, len(current))
	for _, r := range current {
		keep[r.ID] = true
	}
	for id := range cases {
		if !keep[id] {
			delete(cases, id)
		}
	}
	if len(cases) == 0 {
		return nil
	}
	snap := &results.EvalSnapshot{Cases: cases, Summary: results.SummarizeEvalCases(cases)}
	switch {
	case len(fresh) > 0:
		snap.RanAt = now
	case old != nil:
		snap.RanAt = old.RanAt
	}
	return snap
}

func runEval(ctx context.Context, opts EvalOptions, sel provider.Selection, ref plan.UnitRef,
	evalRunner provider.EvalRunner, cli string, c evalspec.Eval, index int, skills []string, baseline bool,
) (evalOutcome, results.EvalResult, error) {
	ctx, span := tracer().Start(ctx, "evolve.case", trace.WithAttributes(
		attribute.String("skill", ref.Skill),
		attribute.String("kind", "evals"),
		attribute.String("label", c.ID),
		attribute.Bool("baseline", baseline),
	))
	defer span.End()

	rep := opts.reporter()
	// A baseline run is not a tree case (it measures the skill's absence), but it
	// runs interleaved right before the eval's own run, so it announces a start —
	// BaselineStarted, distinct from ItemStarted — letting the dashboard flag the
	// row as running its baseline. Its completion still streams via BaselineDone.
	if baseline {
		rep.BaselineStarted(ref, ItemStart{Index: index, Label: c.ID, Runs: 1})
	} else {
		rep.ItemStarted(ref, ItemStart{Index: index, Label: c.ID, Runs: 1})
	}
	copies := make(map[string]string, len(c.Files))
	for _, f := range c.Files {
		copies[f.Dest] = f.Source
	}
	// Evals isolate the skill under test: only it is symlinked in (skills), keeping
	// the agent's per-turn context lean (no sibling-skill descriptions to re-read).
	// The baseline run passes no skills, so the agent faces the same task — same
	// prompt, fixtures, and grading — with the skill absent.
	prefix := "evals."
	if baseline {
		prefix = "baseline."
	}
	parent, keep := opts.retain()
	ws, cleanup, err := workspace.New(parent, prefix, skills,
		unionSkillDirs(opts.Selected), copies, keep)
	if err != nil {
		recordSpanErr(span, err)
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
		Prompt:        c.Prompt,
		MaxTurns:      maxTurns,
		AllowedTools:  c.AllowedTools,
		HostSandboxed: opts.HostSandboxed,
	}, sel.Model.ID)
	spec.Argv[0] = cli

	res, err := opts.Runner.Run(ctx, spec, timeout, nil)
	if err != nil {
		recordSpanErr(span, err)
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
		item := ItemResult{
			Index:         index,
			Label:         c.ID,
			Status:        plan.StatusError,
			Metrics:       plan.ItemMetrics{AvgRunSeconds: &runSeconds},
			WorkspacePath: workdir,
			LogPath:       logPath,
		}
		if baseline {
			item.Detail = fmt.Sprintf("%s: %s runtime failure (exit %d)", c.ID, cli, res.ExitCode)
			rep.BaselineDone(ref, item)
		} else {
			item.Detail = fmt.Sprintf("  [ERROR] %s: %s runtime failure (exit %d): %s\n",
				c.ID, cli, res.ExitCode, errorDetail(reason, res.StderrTail))
			rep.ItemDone(ref, item)
		}
		return outcomeError, results.EvalResult{
			ID:           c.ID,
			Name:         c.Name,
			RuntimeError: reason,
			Timing:       &results.Timing{ExecutorDurationSeconds: &runSeconds},
		}, nil
	}

	// Providers that decode the agent's answer out of a JSON envelope
	// re-materialize any ANSI a tool printed (carried backslash-u escaped in
	// the JSON, so untouched by the runner's capture-time strip) back into raw
	// escape bytes; strip the decoded text so graded evidence and the TUI
	// output stay plain.
	output, usage := evalRunner.ParseEvalOutput(res.Stdout)
	output = ansi.Strip(output)

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
	status := plan.StatusFail
	if evalPassed {
		status = plan.StatusPass
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
	item := ItemResult{
		Index: index, Label: c.ID, Status: status,
		Metrics:       evalItemMetrics(&runSeconds, result.Measured, result.Summary),
		WorkspacePath: workdir,
		LogPath:       logPath,
	}
	if baseline {
		item.Detail = baselineDetail(c.ID, result.Summary)
		rep.BaselineDone(ref, item)
	} else {
		item.Detail = lines.String()
		item.Output = headLines(output, evalOutputLines)
		rep.ItemDone(ref, item)
	}
	outcome := outcomeFail
	if evalPassed {
		outcome = outcomePass
	}
	return outcome, result, nil
}

// baselineDetail is the one-line summary the plain reporter prints for a finished
// baseline eval (the status marker is added by BaselineDone).
func baselineDetail(id string, s *results.GradeSummary) string {
	if s != nil {
		return fmt.Sprintf("%s: %d/%d expectations", id, s.Passed, s.Total)
	}
	return id
}

// evalItemMetrics lifts a finished eval's measured usage and assertion tally
// into the per-case metric pointers the dashboard renders.
func evalItemMetrics(dur *float64, m *results.Measured, s *results.GradeSummary) plan.ItemMetrics {
	im := plan.ItemMetrics{AvgRunSeconds: dur}
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
	contentHash string, entryResults []results.EvalResult, old *results.EvalEntry,
	freshBaseline map[string]results.EvalCaseMetrics,
) *results.EvalEntry {
	header := opts.header(sel, executed)
	header.ContentHash = contentHash
	entry := &results.EvalEntry{
		Header:  header,
		Results: entryResults,
		Summary: results.EvalSummary{Total: len(entryResults)},
	}
	// A real run rotates the replaced entry into Previous and refreshes/carries the
	// baseline; a count-only pass preserves the prior snapshots untouched.
	if executed {
		entry.Previous = results.SnapshotEval(old)
		var oldBaseline *results.EvalSnapshot
		if old != nil {
			oldBaseline = old.Baseline
		}
		entry.Baseline = mergeBaseline(oldBaseline, freshBaseline, entryResults, header.RanAt)
	} else if old != nil {
		entry.Previous = old.Previous
		entry.Baseline = old.Baseline
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
	ms := make([]*results.Measured, len(entryResults))
	for i, r := range entryResults {
		ms[i] = r.Measured
	}
	return results.SumMeasured(ms)
}

// mergeEvalResults builds the saved result list for a (possibly partial) rerun:
// in spec order over the evals valid for this model, it takes the fresh result
// where the eval was just run and the stored result otherwise. This preserves
// evals the rerun did not touch, updates the ones it did, and prunes evals
// removed from the spec (absent from modelApplicable).
func mergeEvalResults(existing *results.EvalEntry, fresh []results.EvalResult,
	modelApplicable []evalspec.Eval,
) []results.EvalResult {
	freshByID := make(map[string]results.EvalResult, len(fresh))
	for _, r := range fresh {
		freshByID[r.ID] = r
	}
	storedByID := map[string]results.EvalResult{}
	if existing != nil {
		for _, r := range existing.Results {
			storedByID[r.ID] = r
		}
	}
	merged := make([]results.EvalResult, 0, len(modelApplicable))
	for _, c := range modelApplicable {
		if r, ok := freshByID[c.ID]; ok {
			merged = append(merged, r)
		} else if r, ok := storedByID[c.ID]; ok {
			merged = append(merged, r)
		}
	}
	return merged
}
