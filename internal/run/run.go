// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"fmt"
	"io"
	"slices"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/runner"
	"github.com/bitwise-media-group/evolve/internal/tokencount"
)

// Runner abstracts agent execution so tests inject fakes; runner.Exec is the
// real implementation.
type Runner interface {
	Run(ctx context.Context, spec provider.CommandSpec, timeout time.Duration,
		onLine func([]byte) bool) (runner.Result, error)
}

// Options holds the sweep configuration the trigger and eval engines share;
// TriggerOptions and EvalOptions embed it and add their engine's knobs.
type Options struct {
	Repo     *layout.Repo
	Selected []provider.Selection
	Counter  *tokencount.Counter
	Runner   Runner
	// PluginFilter and SkillFilter narrow the sweep to the named plugins and
	// skills. An empty list imposes no restriction; a non-empty one keeps only
	// eval sets whose plugin (resp. skill) is listed. The two compose with AND.
	PluginFilter []string
	SkillFilter  []string
	Timeout      time.Duration
	Jobs         int
	MaxTurns     int // agent-turn ceiling per eval; 0 = provider.DefaultMaxTurns. A per-eval max_turns overrides it.
	CountOnly    bool
	// Baseline runs each executed eval a second time with the skill absent, so the
	// report and dashboard can show the skill's lift over no skill at all. The
	// baseline is cached per eval and recomputed only when the eval (spec or
	// fixtures) changes, never when the skill changes; count-only runs skip it.
	Baseline bool
	New      bool
	// Failed selects units that did not pass on a previous run (a complete
	// result graded as failing, or an eval that errored). It composes with New:
	// with both set, a unit reruns when any case is missing data OR previously
	// failed. Like New, selection is per unit (skill/model/tier).
	Failed bool
	// Modified selects cases whose authored content changed since their stored
	// results: a trigger when its SKILL.md frontmatter or its JSON definition
	// changed, an eval when the skill directory contents or its JSON definition
	// changed (detected via fingerprints persisted in the results). It composes
	// with New/Failed by OR, and only fires when a stored result with a baseline
	// fingerprint exists — a brand-new case is New's concern, not Modified's.
	Modified       bool
	KeepWorkspaces bool
	// HostSandboxed reports that Runner wraps each agent in evolve's own OS
	// sandbox, so providers must disable the agent CLI's own sandbox to avoid
	// illegal nesting (threaded into TriggerSpec/EvalSpec). It mirrors the
	// runner's Sandbox.Enabled; the CLI sets both together.
	HostSandboxed bool
	ResultsFormat string // emitted results format: json, jsonc, or yaml ("" = json)
	ToolVersion   string
	Now           func() time.Time
	Stdout        io.Writer
	Stderr        io.Writer

	// Filter narrows the sweep to specific skills and individual
	// triggers/evals on top of the PluginFilter/SkillFilter/EvalFilter and
	// SkipProviders. Nil means no extra narrowing. The TUI selection form
	// builds it; the plain flag path leaves it nil.
	Filter *Filter

	// Reporter receives progress events. When nil the engine uses a
	// PlainReporter over Stdout/Stderr, preserving the historical line output.
	Reporter Reporter

	// RetainRoot, when non-empty, is a directory every workspace is created
	// under and kept (rather than removed at its per-unit cleanup), plus where
	// each eval's full output log is written. The caller owns the root and
	// removes it when finished — the live TUI sets it so the user can open a
	// finished execution's workspace and log. Empty keeps the historical
	// remove-as-you-go behavior and surfaces no paths.
	RetainRoot string
}

// ClearSelectionFlags returns a copy with every per-case selection flag
// (--new/--failed/--modified) cleared. The TUI calls it once the form has
// encoded the user's choice as an explicit per-model Filter: with a Filter
// present those flags must be off, or the engine would re-derive the run-set and
// override what the user picked. Clearing them all in one place means a new
// selection flag cannot silently leak past the form into the engine.
func (o Options) ClearSelectionFlags() Options {
	o.New = false
	o.Failed = false
	o.Modified = false
	return o
}

// selects reports whether an eval set's plugin and skill pass the CLI-level
// --plugin/--skill filters. An empty filter list matches every value; a
// non-empty one requires membership. The two compose with AND.
func (o Options) selects(plugin, skill string) bool {
	return inFilter(o.PluginFilter, plugin) && inFilter(o.SkillFilter, skill)
}

// inFilter reports whether v passes a filter list: an empty list imposes no
// restriction, otherwise v must appear in it.
func inFilter(list []string, v string) bool {
	if len(list) == 0 {
		return true
	}
	return slices.Contains(list, v)
}

// retain reports the parent directory new workspaces are created under and
// whether they must outlive their per-unit cleanup. Retention is on whenever a
// RetainRoot is set (the TUI) or the user passed --keep-workspaces.
func (o Options) retain() (parent string, keep bool) {
	return o.RetainRoot, o.KeepWorkspaces || o.RetainRoot != ""
}

// retainedDir is the workspace path to surface to the TUI: ws while it is being
// retained, "" when it is about to be removed (so the TUI shows no open hint).
func retainedDir(root, ws string) string {
	if root == "" {
		return ""
	}
	return ws
}

// ensureReporter materializes the default PlainReporter once, before any parallel
// fan-out. Without it reporter() would mint a fresh PlainReporter per call, and
// the per-instance write lock NewPlainReporter installs could not serialize the
// concurrent agent-run goroutines against each other. Each engine entry point
// calls it first thing; a caller-supplied Reporter (the TUI) is left untouched.
func (o *Options) ensureReporter() {
	if o.Reporter == nil {
		o.Reporter = NewPlainReporter(o.Stdout, o.Stderr)
	}
}

// reporter returns the configured reporter, defaulting to a PlainReporter that
// reproduces the historical stdout/stderr output. Callers reach it after
// ensureReporter has populated Reporter, so concurrent callers share one instance.
func (o *Options) reporter() Reporter {
	if o.Reporter != nil {
		return o.Reporter
	}
	return NewPlainReporter(o.Stdout, o.Stderr)
}

// header snapshots the run metadata every results entry records.
func (o *Options) header(sel provider.Selection, executed bool) results.Header {
	return results.Header{
		Provider:       sel.Provider.Name(),
		Model:          sel.Model.ID,
		Display:        sel.Model.Display,
		ToolVersion:    o.ToolVersion,
		RanAt:          o.Now().UTC().Format(time.RFC3339),
		Executed:       executed,
		TimeoutSeconds: int(o.Timeout.Seconds()),
		Pricing:        results.PricingOf(sel.Model.InputUSD, sel.Model.OutputUSD),
	}
}

func payload(skillMD []byte, prompt string) string {
	return string(skillMD) + "\n\n" + prompt
}

// countTokens counts the input tokens for each text, returning the counts
// positionally: out[i] is the count for texts[i], or nil when the provider has
// no counting API or the call fails (Counter.Count swallows those into nil).
//
// On a cache miss each Count is a network round-trip to the provider's counting
// API, and a SKILL.md edit invalidates every prior key, so the common iterate-
// and-rerun case misses for all cases at once. Run sequentially that stalls each
// skill/model unit for one round-trip per case before any agent run starts —
// the dominant cost of the inter-unit gap, not the sub-millisecond results
// rewrite. Fanning the calls out over the same opts.Jobs budget the agent runs
// use collapses that to roughly a single round-trip; Counter is safe for
// concurrent use.
func (o *Options) countTokens(ctx context.Context, sel provider.Selection, texts []string) []*int {
	out := make([]*int, len(texts))
	limit := max(o.Jobs, 1)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
	for i, text := range texts {
		g.Go(func() error {
			out[i] = o.Counter.Count(gctx, sel.Provider, sel.Model.ID, text)
			return nil
		})
	}
	_ = g.Wait() // Count never returns an error; Wait is just the join.
	return out
}

// warnSkillName flags an authored skill_name that contradicts the directory
// the eval set lives under; the directory name stays authoritative.
func warnSkillName(opts *Options, set layout.EvalSet, path, authored string) {
	if authored != "" && authored != set.Skill {
		opts.reporter().Warn("  warn: %s: skill_name %q does not match skill directory %q\n",
			opts.Repo.Rel(path), authored, set.Skill)
	}
}

func unionSkillDirs(selected []provider.Selection) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, sel := range selected {
		for _, d := range sel.Provider.SkillDirs() {
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}
	return dirs
}

func avgSuffix(avg *float64) string {
	if avg == nil {
		return ""
	}
	return fmt.Sprintf(", avg run %.1fs", *avg)
}
