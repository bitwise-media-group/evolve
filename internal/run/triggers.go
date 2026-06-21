// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sync/errgroup"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/workspace"
)

// TriggerOptions configures a trigger sweep.
type TriggerOptions struct {
	Options
	Runs int
}

// Triggers executes the sweep. failed reports whether any executed query
// failed; err reports interruption or setup problems.
func Triggers(ctx context.Context, opts TriggerOptions) (failed bool, err error) {
	sets, err := opts.Repo.EvalSets()
	if err != nil {
		return false, err
	}
	for _, set := range sets {
		if set.TriggersPath == "" || !opts.selects(set.Plugin.Name, set.Skill) {
			continue
		}
		setFailed, err := runTriggerSet(ctx, opts, set)
		failed = failed || setFailed
		if err != nil {
			return failed, err
		}
	}
	return failed, nil
}

func runTriggerSet(ctx context.Context, opts TriggerOptions, set layout.EvalSet) (failed bool, err error) {
	spec, err := evalspec.LoadTriggers(set.TriggersPath)
	if err != nil {
		return false, err
	}
	warnSkillName(&opts.Options, set, set.TriggersPath, spec.SkillName)
	triggers := spec.Triggers
	skillMD, err := os.ReadFile(filepath.Join(set.SkillDir, "SKILL.md"))
	if err != nil {
		return false, fmt.Errorf("reading skill under test: %w", err)
	}
	// A trigger eval depends on the skill's frontmatter (what makes it trigger),
	// so that is the content fingerprint persisted for --modified detection.
	contentHash := triggerContentHash(skillMD)
	rep := opts.reporter()
	file, reset := results.LoadDir(set.ResultsDir, set.Plugin.Name, set.Skill)
	if reset {
		rep.Warn("  warn: %s has an old or unreadable results schema; starting fresh (schema %d)\n",
			opts.Repo.Rel(results.Find(set.ResultsDir)), results.Schema)
	}

	// Triggers need every sibling skill present: the test is whether the model
	// activates the right one among the alternatives.
	skills, err := workspace.SkillDirs(set.Plugin.SkillsDir)
	if err != nil {
		return false, err
	}
	parent, keep := opts.retain()
	ws, cleanup, err := workspace.New(parent, "triggers.", skills,
		unionSkillDirs(opts.Selected), nil, keep)
	if err != nil {
		return false, err
	}
	defer cleanup()
	if opts.KeepWorkspaces {
		rep.Warn("  workspace kept: %s\n", ws)
	}

	for _, sel := range opts.Selected {
		unitFailed, err := runTriggerUnit(ctx, opts, set, sel, file, skillMD, contentHash, triggers, ws)
		failed = failed || unitFailed
		if err != nil {
			return failed, err
		}
	}
	return failed, nil
}

// runTriggerUnit runs one (skill, model) trigger unit against the shared results
// file, skill payload, and read-only trigger workspace. Under --new/--failed it
// reruns only the queries with a gap, merging their fresh results back over the
// stored ones; otherwise it runs every selected query. A unit with no applicable
// queries reports nothing and returns cleanly.
func runTriggerUnit(ctx context.Context, opts TriggerOptions, set layout.EvalSet, sel provider.Selection,
	file *results.File, skillMD []byte, contentHash string, triggers []evalspec.Trigger, ws string,
) (failed bool, err error) {

	rep := opts.reporter()
	provName := sel.Provider.Name()
	// modelApplicable is every query valid for this model (skip_providers + skill
	// only), ignoring the selection filter, so a partial rerun can preserve the
	// queries it does not touch. applicable then narrows by the selection filter.
	modelApplicable := applicableTriggers(triggers, provName, set.Skill, nil)
	applicable := applicableTriggers(triggers, provName, set.Skill, opts.Filter)
	if len(applicable) == 0 {
		return false, nil
	}
	ref := UnitRef{Skill: set.Skill, Key: sel.Key(), Kind: KindTriggers}
	cli, cliFound := provider.ResolveCLI(sel.Provider)
	execute := !opts.CountOnly && cliFound

	// Per-case run-set: under --new/--failed/--modified keep only the queries
	// with a gap. reason != ReasonNone is the same predicate the TUI form
	// preselects on, so CLI and TUI run the identical set.
	runSet := applicable
	if opts.New || opts.Failed || opts.Modified {
		runSet = nil
		for _, t := range applicable {
			r, storedContent, ok := lookupTrigger(file, sel.Key(), t.Query)
			fp := fingerprints{storedContent: storedContent, freshContent: contentHash, freshSpec: specHash(t)}
			if triggerCaseReason(r, ok, execute, opts.New, opts.Failed, opts.Modified, fp) != ReasonNone {
				runSet = append(runSet, t)
			}
		}
		if len(runSet) == 0 {
			rep.UnitSkipped(ref, "results complete")
			return false, nil
		}
	}
	if !execute && !opts.CountOnly {
		rep.Warn("  warn: `%s` CLI not found; %s gets token counts only\n",
			sel.Provider.CLI()[0], sel.Key())
	}

	mode := ModeCountOnly
	if execute {
		mode = ModeRun
	}
	rep.UnitStarted(ref, len(runSet), opts.Runs, mode)

	// Token counting stays on this goroutine (cache-cheap); only agent runs
	// go parallel.
	entryResults := make([]results.TriggerResult, len(runSet))
	for i, t := range runSet {
		tokens := opts.Counter.Count(ctx, sel.Provider, sel.Model.ID, payload(skillMD, t.Query))
		entryResults[i] = results.TriggerResult{
			Query:         t.Query,
			ShouldTrigger: t.ShouldTrigger,
			Estimate:      results.NewEstimate(tokens, sel.Model.InputUSD),
			SpecHash:      specHash(t),
		}
	}
	if execute {
		batchFailed, err := runQueries(ctx, opts, sel, cli, ws, ref, runSet, entryResults)
		failed = failed || batchFailed
		if err != nil {
			return failed, err
		}
	}

	merged := mergeTriggerResults(file.Triggers[sel.Key()], entryResults, modelApplicable)
	entry := buildTriggerEntry(opts, sel, execute, contentHash, merged)
	file.SetTrigger(sel.Key(), entry)
	saved, err := file.SaveDir(set.ResultsDir, opts.ResultsFormat)
	if err != nil {
		return failed, err
	}
	sum := UnitSummary{Executed: entry.Executed, Total: entry.Summary.Total}
	if entry.Executed {
		sum.Passed = *entry.Summary.Passed
		sum.AvgRunSeconds = entry.Summary.AvgRunSeconds
	}
	rep.UnitFinished(ref, sum, opts.Repo.Rel(saved))
	return failed, nil
}

// runQueries executes every query's runs concurrently (jobs at a time) and
// fills hits/runs/passed/avg into entryResults as queries complete. Sharing
// the workspace is safe: trigger sessions are read-only.
func runQueries(ctx context.Context, opts TriggerOptions, sel provider.Selection, cli, ws string, ref UnitRef,
	triggers []evalspec.Trigger, entryResults []results.TriggerResult) (bool, error) {

	rep := opts.reporter()
	skill := ref.Skill
	type outcome struct {
		index   int
		hit     bool
		seconds float64
	}
	outcomes := make(chan outcome)

	collectorDone := make(chan bool)
	go func() {
		hits := make([]int, len(triggers))
		elapsed := make([]float64, len(triggers))
		remaining := make([]int, len(triggers))
		for i := range remaining {
			remaining[i] = opts.Runs
		}
		failed := false
		for o := range outcomes {
			if o.hit {
				hits[o.index]++
			}
			elapsed[o.index] += o.seconds
			remaining[o.index]--
			if remaining[o.index] > 0 {
				continue
			}
			i := o.index
			rate := float64(hits[i]) / float64(opts.Runs)
			avg := results.Round1(elapsed[i] / float64(opts.Runs))
			expected := triggers[i].ShouldTrigger
			passed := rate < 0.5
			if expected {
				passed = rate >= 0.5
			}
			failed = failed || !passed
			h, r := hits[i], opts.Runs
			entryResults[i].Hits = &h
			entryResults[i].Runs = &r
			entryResults[i].Passed = &passed
			entryResults[i].AvgRunSeconds = &avg

			status, expect := StatusPass, "no"
			if !passed {
				status = StatusFail
			}
			if expected {
				expect = "yes"
			}
			rep.ItemDone(ref, ItemResult{
				Index:  i,
				Label:  triggers[i].Query,
				Status: status,
				Detail: fmt.Sprintf("rate=%.2f avg=%.1fs expect=%s %s",
					rate, avg, expect, truncate(triggers[i].Query, 70)),
				Metrics: ItemMetrics{
					Hits: &h, Runs: &r, AvgRunSeconds: &avg,
					InputTokens: estTokens(entryResults[i].Estimate),
					CostUSD:     estCost(entryResults[i].Estimate),
				},
				// Triggers share one read-only skill workspace and capture no
				// per-query output, so surface the dir (o) but no log (l).
				WorkspacePath: retainedDir(opts.RetainRoot, ws),
			})
		}
		collectorDone <- failed
	}()

	g, runCtx := errgroup.WithContext(ctx)
	g.SetLimit(opts.Jobs)
	for i, t := range triggers {
		rep.ItemStarted(ref, ItemStart{Index: i, Label: t.Query, Runs: opts.Runs})
		for range opts.Runs {
			g.Go(func() error {
				spec := sel.Provider.TriggerSpec(ws, t.Query, sel.Model.ID, opts.HostSandboxed)
				spec.Argv[0] = cli
				onLine := func(line []byte) bool {
					hit, note := sel.Provider.ScanLine(line, skill)
					if note != "" {
						rep.Warn("  warn: %s\n", note)
					}
					return hit
				}
				res, err := opts.Runner.Run(runCtx, spec, opts.Timeout, onLine)
				if err != nil {
					return err
				}
				if res.TimedOut {
					detail := ""
					if res.StderrTail != "" {
						detail = "; stderr tail: " + tail(res.StderrTail, 300)
					}
					rep.Warn("  warn: runner timed out; counted as no-trigger%s\n", detail)
				}
				outcomes <- outcome{index: i, hit: res.Hit, seconds: res.Elapsed.Seconds()}
				return nil
			})
		}
	}
	err := g.Wait()
	close(outcomes)
	failed := <-collectorDone
	return failed, err
}

func buildTriggerEntry(opts TriggerOptions, sel provider.Selection, executed bool,
	contentHash string, entryResults []results.TriggerResult) *results.TriggerEntry {
	header := opts.header(sel, executed)
	header.ContentHash = contentHash
	entry := &results.TriggerEntry{
		Header:  header,
		Results: entryResults,
		Summary: results.TriggerSummary{Total: len(entryResults)},
	}
	if executed {
		entry.RunsPerQuery = opts.Runs
		passed, failed := 0, 0
		var runSum float64
		var runCount int
		for _, r := range entryResults {
			if r.Passed != nil {
				if *r.Passed {
					passed++
				} else {
					failed++
				}
			}
			if r.AvgRunSeconds != nil {
				runSum += *r.AvgRunSeconds
				runCount++
			}
		}
		entry.Summary.Passed = &passed
		entry.Summary.Failed = &failed
		if passed+failed > 0 {
			rate := results.Round6(float64(passed) / float64(passed+failed))
			entry.Summary.PassRate = &rate
		}
		if runCount > 0 {
			avg := results.Round1(runSum / float64(runCount))
			entry.Summary.AvgRunSeconds = &avg
		}
	}
	estimates := make([]*results.Estimate, len(entryResults))
	for i, r := range entryResults {
		estimates[i] = r.Estimate
	}
	entry.Summary.Estimate = results.SumEstimates(estimates)
	return entry
}

// mergeTriggerResults builds the saved result list for a (possibly partial)
// rerun: in spec order over the queries valid for this model, it takes the fresh
// result where the query was just run and the stored result otherwise. This
// preserves queries the rerun did not touch, updates the ones it did, and prunes
// queries removed from the spec (absent from modelApplicable).
func mergeTriggerResults(existing *results.TriggerEntry, fresh []results.TriggerResult,
	modelApplicable []evalspec.Trigger) []results.TriggerResult {

	freshByQuery := make(map[string]results.TriggerResult, len(fresh))
	for _, r := range fresh {
		freshByQuery[r.Query] = r
	}
	storedByQuery := map[string]results.TriggerResult{}
	if existing != nil {
		for _, r := range existing.Results {
			storedByQuery[r.Query] = r
		}
	}
	merged := make([]results.TriggerResult, 0, len(modelApplicable))
	for _, t := range modelApplicable {
		if r, ok := freshByQuery[t.Query]; ok {
			merged = append(merged, r)
		} else if r, ok := storedByQuery[t.Query]; ok {
			merged = append(merged, r)
		}
	}
	return merged
}

func applicableTriggers(triggers []evalspec.Trigger, providerName, skill string, f *Filter) []evalspec.Trigger {
	if !f.skillIncluded(skill) {
		return nil
	}
	var out []evalspec.Trigger
	for _, t := range triggers {
		if t.SkipsProvider(providerName) {
			continue
		}
		if !f.triggerIncluded(skill, t.Query) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// estTokens and estCost lift an estimate's input figures into the optional
// per-case metric pointers, tolerating a nil estimate (no counting API).
func estTokens(e *results.Estimate) *int {
	if e == nil {
		return nil
	}
	n := e.InputTokens
	return &n
}

func estCost(e *results.Estimate) *float64 {
	if e == nil {
		return nil
	}
	return e.InputCostUSD
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
