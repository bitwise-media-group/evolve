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
		if set.TriggersPath == "" || (opts.SkillFilter != "" && set.Skill != opts.SkillFilter) {
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
	rep := opts.reporter()
	file, reset := results.LoadDir(set.ResultsDir, set.Plugin.Name, set.Skill)
	if reset {
		rep.Warn("  warn: %s has an old or unreadable results schema; starting fresh (schema %d)\n",
			opts.Repo.Rel(results.Find(set.ResultsDir)), results.Schema)
	}

	ws, cleanup, err := workspace.New("triggers.", set.Plugin.SkillsDir,
		unionSkillDirs(opts.Selected), nil, opts.KeepWorkspaces)
	if err != nil {
		return false, err
	}
	defer cleanup()
	if opts.KeepWorkspaces {
		rep.Warn("  workspace kept: %s\n", ws)
	}

	for _, sel := range opts.Selected {
		applicable := applicableTriggers(triggers, sel.Provider.Name(), set.Skill, opts.Filter)
		if len(applicable) == 0 {
			continue
		}
		ref := UnitRef{Skill: set.Skill, Key: sel.Key(), Kind: KindTriggers}
		cli, cliFound := provider.ResolveCLI(sel.Provider)
		execute := !opts.CountOnly && cliFound

		probe := func(t evalspec.Trigger) bool {
			return opts.Counter.Count(ctx, sel.Provider, sel.Model.ID, payload(skillMD, t.Query)) != nil
		}
		_, countCapable := sel.Provider.(provider.TokenCounter)
		if opts.New {
			if reason := triggerSkipReason(
				file.Triggers[sel.Key()], applicable, sel.Model, execute, countCapable, probe,
			); reason != "" {
				rep.UnitSkipped(ref, reason)
				continue
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
		rep.UnitStarted(ref, len(applicable), opts.Runs, mode)

		// Token counting stays on this goroutine (cache-cheap); only agent
		// runs go parallel.
		entryResults := make([]results.TriggerResult, len(applicable))
		for i, t := range applicable {
			tokens := opts.Counter.Count(ctx, sel.Provider, sel.Model.ID, payload(skillMD, t.Query))
			entryResults[i] = results.TriggerResult{
				Query:         t.Query,
				ShouldTrigger: t.ShouldTrigger,
				Estimate:      results.NewEstimate(tokens, sel.Model.InputUSD),
			}
		}
		if execute {
			batchFailed, err := runQueries(ctx, opts, sel, cli, ws, ref, applicable, entryResults)
			failed = failed || batchFailed
			if err != nil {
				return failed, err
			}
		}

		entry := buildTriggerEntry(opts, sel, execute, entryResults)
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
	}
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
			})
		}
		collectorDone <- failed
	}()

	g, runCtx := errgroup.WithContext(ctx)
	g.SetLimit(opts.Jobs)
	for i, t := range triggers {
		for range opts.Runs {
			g.Go(func() error {
				spec := sel.Provider.TriggerSpec(ws, t.Query, sel.Model.ID)
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
	entryResults []results.TriggerResult) *results.TriggerEntry {
	entry := &results.TriggerEntry{
		Header:  opts.header(sel, executed),
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

// triggerSkipReason is why --new may skip this skill/model, or "" when a
// (re)run is needed. Fields a run could never fill are exempt: cost for
// models without published pricing, execution fields when the runner is
// unavailable or this invocation is count-only, and token counts the counting
// API cannot produce (probe reports that).
func triggerSkipReason(entry *results.TriggerEntry, triggers []evalspec.Trigger, model provider.Model,
	execute, countCapable bool, probe func(evalspec.Trigger) bool) string {

	stored := map[string]results.TriggerResult{}
	if entry != nil {
		for _, r := range entry.Results {
			stored[r.Query] = r
		}
	}
	var uncounted *evalspec.Trigger
	for _, t := range triggers {
		r, ok := stored[t.Query]
		if !ok || r.ShouldTrigger != t.ShouldTrigger {
			return ""
		}
		if execute && (r.Hits == nil || r.Runs == nil || r.Passed == nil || r.AvgRunSeconds == nil) {
			return ""
		}
		// Estimates a provider can never produce (no counting API) are exempt.
		missingCount := countCapable && (r.Estimate == nil ||
			(model.InputUSD != nil && r.Estimate.InputCostUSD == nil))
		if uncounted == nil && missingCount {
			uncounted = &t
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
