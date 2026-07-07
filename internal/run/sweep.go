// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/workspace"
)

// SweepOptions configures an interleaved triggers+evals sweep: for each skill in
// repository order, every selected model runs its triggers then its evals before
// the next model, and the next skill follows only once the current one is done.
// This is the execution order the live dashboard mirrors — a skill/model pair
// finishes both tiers before the run moves on, rather than running all triggers
// across everything and then all evals.
type SweepOptions struct {
	Options
	Tiers      plan.Tiers
	Runs       int    // runs per query (triggers tier)
	EvalFilter string // restrict evals to a single id ("" = all)
	JudgeModel string

	// Per-tier timeouts; each falls back to Options.Timeout when zero.
	TriggerTimeout time.Duration
	EvalTimeout    time.Duration

	// Filters narrows the run per resolved model, keyed by Selection.Key(). When
	// non-nil, a model whose entry is nil is skipped entirely — the TUI uses this
	// to honor --new per model. When nil, Options.Filter applies to every model.
	Filters map[string]*plan.Filter
}

// Sweep runs the configured tiers in execution order: skill → model → tiers.
// failed reports whether any executed case failed; err reports interruption or
// setup problems.
func Sweep(ctx context.Context, opts SweepOptions) (failed bool, err error) {
	ctx, span := tracer().Start(ctx, "evolve.sweep", trace.WithAttributes(
		attribute.String("command", "sweep"),
		attribute.Bool("triggers", opts.Tiers.Triggers),
		attribute.Bool("evals", opts.Tiers.Evals),
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
		if !opts.selects(set.Plugin.Name, set.Skill) {
			continue
		}
		setFailed, err := runSweepSet(ctx, opts, set)
		failed = failed || setFailed
		if err != nil {
			return failed, err
		}
	}
	return failed, nil
}

// runSweepSet runs every selected model's triggers then evals for one skill,
// sharing a single results file, skill payload, and read-only trigger workspace.
func runSweepSet(ctx context.Context, opts SweepOptions, set layout.EvalSet) (failed bool, err error) {
	ctx, span := tracer().Start(ctx, "evolve.skill_set", trace.WithAttributes(
		attribute.String("plugin", set.Plugin.Name),
		attribute.String("skill", set.Skill),
	))
	defer func() { endSpan(span, err) }()

	runTriggers := opts.Tiers.Triggers && set.TriggersPath != ""
	runEvalsTier := opts.Tiers.Evals && set.EvalsPath != ""
	if !runTriggers && !runEvalsTier {
		return false, nil
	}

	var triggers []evalspec.Trigger
	if runTriggers {
		spec, err := evalspec.LoadTriggers(set.TriggersPath)
		if err != nil {
			return false, err
		}
		warnSkillName(&opts.Options, set, set.TriggersPath, spec.SkillName)
		triggers = spec.Triggers
	}
	var evals []evalspec.Eval
	var allowedModels []string
	if runEvalsTier {
		spec, err := evalspec.LoadEvals(set.EvalsPath)
		if err != nil {
			return false, err
		}
		warnSkillName(&opts.Options, set, set.EvalsPath, spec.SkillName)
		allowedModels = spec.Models
		evals = filterEvals(spec.Evals, opts.EvalFilter)
		runEvalsTier = len(evals) > 0
	} else if runTriggers && set.EvalsPath != "" {
		// Triggers honor the skill's models restriction (defined on the evals
		// file) even when the evals tier is not part of this sweep.
		if ef, err := evalspec.LoadEvals(set.EvalsPath); err == nil {
			allowedModels = ef.Models
		}
	}
	if !runTriggers && !runEvalsTier {
		return false, nil // both tiers narrowed away (e.g. --eval matched nothing)
	}

	file, skillMD, err := loadSet(opts.Options, set)
	if err != nil {
		return false, err
	}
	// Content fingerprints persisted for --modified: a trigger entry hashes the
	// SKILL.md frontmatter, an eval entry the whole skill directory.
	var triggerContent, evalContent string
	if runTriggers {
		triggerContent = triggerContentHash(skillMD)
	}
	if runEvalsTier {
		if evalContent, err = skillContentHash(set.SkillDir); err != nil {
			return false, fmt.Errorf("fingerprinting skill under test: %w", err)
		}
	}
	rep := opts.reporter()

	// The trigger sweep shares one read-only workspace across every model;
	// evals stage their own per-eval workspaces.
	var ws string
	if runTriggers {
		// Triggers need every sibling skill present (the model must pick the
		// right one); evals isolate the skill under test in runEval.
		skills, err := workspace.SkillDirs(set.Plugin.SkillsDir)
		if err != nil {
			return false, err
		}
		parent, keep := opts.retain()
		w, cleanup, err := workspace.New(parent, "triggers.", skills,
			unionSkillDirs(opts.Selected), nil, keep)
		if err != nil {
			return false, err
		}
		defer cleanup()
		if opts.KeepWorkspaces {
			rep.Warn("  workspace kept: %s\n", w)
		}
		ws = w
	}

	u := sweepUnit{
		opts: opts, set: set, file: file, skillMD: skillMD,
		triggers: triggers, evals: evals, allowedModels: allowedModels,
		triggerContent: triggerContent, evalContent: evalContent,
		ws: ws, runTriggers: runTriggers, runEvalsTier: runEvalsTier,
	}
	for _, sel := range opts.Selected {
		modelFailed, err := runSweepModel(ctx, u, sel)
		failed = failed || modelFailed
		if err != nil {
			return failed, err
		}
	}
	return failed, nil
}

// sweepUnit carries the per-skill invariants every model in a sweep set shares,
// so runSweepModel is one helper rather than two inline tier blocks per model.
type sweepUnit struct {
	opts                        SweepOptions
	set                         layout.EvalSet
	file                        *results.File
	skillMD                     []byte
	triggers                    []evalspec.Trigger
	evals                       []evalspec.Eval
	allowedModels               []string
	triggerContent, evalContent string
	ws                          string
	runTriggers, runEvalsTier   bool
}

// runSweepModel runs one model's triggers then evals within a sweep set. A nil
// per-model filter (the model is already complete under --new) skips it cleanly.
func runSweepModel(ctx context.Context, u sweepUnit, sel harness.Selection) (failed bool, err error) {
	filter := u.opts.Filter
	if u.opts.Filters != nil {
		filter = u.opts.Filters[sel.Key()]
		if filter == nil {
			return false, nil
		}
	}
	if u.runTriggers {
		to := u.opts.Options
		to.Filter = filter
		to.Timeout = pickTimeout(u.opts.TriggerTimeout, u.opts.Timeout)
		tf, err := runTriggerUnit(ctx, TriggerOptions{Options: to, Runs: u.opts.Runs},
			u.set, sel, u.file, u.skillMD, u.triggerContent, u.triggers, u.allowedModels, u.ws)
		failed = failed || tf
		if err != nil {
			return failed, err
		}
	}
	if u.runEvalsTier {
		eo := u.opts.Options
		eo.Filter = filter
		eo.Timeout = pickTimeout(u.opts.EvalTimeout, u.opts.Timeout)
		ef, err := runEvalUnit(ctx, EvalOptions{Options: eo, EvalFilter: u.opts.EvalFilter, JudgeModel: u.opts.JudgeModel},
			u.set, sel, u.file, u.skillMD, u.evalContent, u.evals, u.allowedModels)
		failed = failed || ef
		if err != nil {
			return failed, err
		}
	}
	return failed, nil
}

func pickTimeout(tier, fallback time.Duration) time.Duration {
	if tier > 0 {
		return tier
	}
	return fallback
}

// filterEvals restricts a skill's evals to a single id, mirroring --eval; an
// empty id keeps them all.
func filterEvals(evals []evalspec.Eval, id string) []evalspec.Eval {
	if id == "" {
		return evals
	}
	var out []evalspec.Eval
	for _, c := range evals {
		if c.ID == id {
			out = append(out, c)
		}
	}
	return out
}

// loadSet loads the shared per-skill state every unit reads and writes: the
// results file (warning on a discarded incompatible schema) and the skill payload.
func loadSet(opts Options, set layout.EvalSet) (*results.File, []byte, error) {
	rep := opts.reporter()
	file, reset, err := results.LoadDir(set.ResultsDir, set.Plugin.Name, set.Skill)
	if err != nil {
		return nil, nil, err
	}
	if reset {
		rep.Warn("  warn: %s has an old or unreadable results schema; starting fresh (schema %d)\n",
			opts.Repo.Rel(results.Find(set.ResultsDir)), results.Schema)
	}
	skillMD, err := os.ReadFile(filepath.Join(set.SkillDir, "SKILL.md"))
	if err != nil {
		return nil, nil, fmt.Errorf("reading skill under test: %w", err)
	}
	return file, skillMD, nil
}
