// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// SweepOptions configures a remote sweep. Options carries the shared engine
// configuration (repo, selections, filters, per-run knobs); the sweep plans
// with AssumeRunnable — eligibility is the server's concern — and never
// executes anything locally.
type SweepOptions struct {
	run.Options
	Client *Client
	Tiers  plan.Tiers
	// Runs per trigger query.
	Runs int
	// EvalFilter restricts evals to one id ("" = all).
	EvalFilter string
	// Per-tier timeouts, falling back to Options.Timeout.
	TriggerTimeout time.Duration
	EvalTimeout    time.Duration
	// Judge is the resolved judge model (zero = no judge); the pod binds it
	// on the unit's own harness.
	Judge model.Model
	// ClientVersion recorded on every unit.
	ClientVersion string
}

// plannedUnit is one unit before submission, with everything needed to land
// its result afterwards.
type plannedUnit struct {
	set    layout.EvalSet
	sel    harness.Selection
	kind   plan.Kind
	bundle *Bundle
	spec   UnitSpec
}

// Sweep plans, submits, and monitors a remote run: enumerate units exactly
// as run.Sweep would, compute the case selection locally, bundle and dedupe
// workspaces, submit, then stream — progress events replay onto the
// Reporter, and every settled unit's entry merges into the local results
// file, saved with the same rotation a local run performs. failed reports
// graded failures (the --strict signal).
func Sweep(ctx context.Context, opts SweepOptions) (failed bool, err error) {
	rep := opts.Reporter
	if rep == nil {
		rep = run.NewPlainReporter(opts.Stdout, opts.Stderr)
	}

	units, err := planUnits(&opts)
	if err != nil {
		return false, err
	}
	if len(units) == 0 {
		rep.Warn("nothing to run remotely (no applicable units, or everything is up to date)")
		return false, nil
	}

	if err := uploadBundles(ctx, opts.Client, units); err != nil {
		return false, err
	}

	specs := make([]UnitSpec, len(units))
	for i := range units {
		specs[i] = units[i].spec
	}
	sub, err := opts.Client.Submit(ctx, &Submission{Version: SubmissionVersion, Units: specs})
	if err != nil {
		return false, err
	}
	rep.Warn("submitted %d units to %s as %s", len(units), opts.Client.BaseURL, sub.Name)

	unitByName := map[string]*plannedUnit{}
	for i, name := range sub.Units {
		if i < len(units) {
			unitByName[name] = &units[i]
		}
	}

	// Ctrl-C cancels server-side with a fresh context — the interrupted one
	// is already dead.
	defer func() {
		if ctx.Err() != nil {
			cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			if cerr := opts.Client.Cancel(cancelCtx, sub.Name); cerr == nil {
				rep.Warn("cancelled %s", sub.Name)
			}
		}
	}()

	landed := map[string]bool{}
	started := map[string]bool{}
	onUnit := func(u UnitStatusWire) {
		pu := unitByName[u.Name]
		if pu == nil {
			return
		}
		ref := plan.UnitRef{Skill: pu.set.Skill, Key: pu.sel.Key(), Kind: pu.kind}
		if u.Phase == "Running" && !started[u.Name] {
			started[u.Name] = true
			rep.UnitStarted(ref, 0, opts.Runs, plan.ModeRun)
		}
		if landed[u.Name] || (u.Phase != "Complete" && u.Phase != "Failed") {
			return
		}
		landed[u.Name] = true
		if fail, lerr := landUnit(pu, u, rep, opts.ResultsFormat); lerr != nil {
			failed = true
			rep.Warn("unit %s: %v", u.Name, lerr)
		} else if fail {
			failed = true
		}
	}
	onEvent := func(ev EventWire) { ApplyEvent(rep, ev.Event) }

	final, err := opts.Client.Events(ctx, sub.Name, onUnit, onEvent)
	if err != nil {
		return failed, err
	}
	for _, u := range final.Units {
		onUnit(u) // idempotent: lands anything the stream did not deliver
	}
	if final.Phase == "Failed" {
		failed = true
	}
	return failed, nil
}

// planUnits enumerates skill × model × tier exactly as run.Sweep does, with
// the case run-set computed via the engine's own predicates.
func planUnits(opts *SweepOptions) ([]plannedUnit, error) {
	cat, err := run.Catalog(opts.Options)
	if err != nil {
		return nil, err
	}
	need, _ := run.Needs(opts.Options, cat, opts.Selected, opts.Tiers, opts.EvalFilter)
	flags := opts.New || opts.Failed || opts.Modified

	sets, err := opts.Repo.EvalSets()
	if err != nil {
		return nil, err
	}
	catBySkill := map[string]plan.SkillCatalog{}
	for _, sc := range cat {
		catBySkill[sc.Plugin+"\x00"+sc.Skill] = sc
	}

	triggerBundles := map[string]*Bundle{}
	var units []plannedUnit
	for _, set := range sets {
		sc, ok := catBySkill[set.Plugin.Name+"\x00"+set.Skill]
		if !ok {
			continue
		}
		for _, sel := range opts.Selected {
			if !sc.Allows(sel.Model) {
				continue
			}
			if opts.Tiers.Triggers && set.TriggersPath != "" {
				unit, err := planOne(opts, set, sel, plan.KindTriggers, sc, need, flags, triggerBundles)
				if err != nil {
					return nil, err
				}
				if unit != nil {
					units = append(units, *unit)
				}
			}
			if opts.Tiers.Evals && set.EvalsPath != "" {
				unit, err := planOne(opts, set, sel, plan.KindEvals, sc, need, flags, nil)
				if err != nil {
					return nil, err
				}
				if unit != nil {
					units = append(units, *unit)
				}
			}
		}
	}
	return units, nil
}

// planOne renders one unit (or nil when its run-set is empty under the
// selection flags).
func planOne(opts *SweepOptions, set layout.EvalSet, sel harness.Selection, kind plan.Kind,
	sc plan.SkillCatalog, need map[string]map[plan.CaseRef]bool, flags bool,
	triggerBundles map[string]*Bundle) (*plannedUnit, error) {

	cases, any := runSet(sc, sel, kind, need, flags, opts.EvalFilter)
	if !any {
		return nil, nil
	}

	var bundle *Bundle
	var err error
	if kind == plan.KindTriggers {
		bundle = triggerBundles[set.Plugin.Name]
		if bundle == nil {
			if bundle, err = TriggersBundle(set.Plugin); err != nil {
				return nil, err
			}
			triggerBundles[set.Plugin.Name] = bundle
		}
	} else if bundle, err = EvalsBundle(set); err != nil {
		return nil, err
	}

	tier := 1
	timeout := opts.TriggerTimeout
	if kind == plan.KindEvals {
		tier = 2
		timeout = opts.EvalTimeout
	}
	if timeout == 0 {
		timeout = opts.Timeout
	}

	spec := UnitSpec{
		Skill:         set.Skill,
		Plugin:        set.Plugin.Name,
		Tier:          tier,
		Model:         sel.Model.ID,
		Harnesses:     harnessOptions(sel.Model),
		Workspace:     WorkspaceRef{Digest: bundle.Digest, SizeBytes: int64(len(bundle.Data))},
		TimeoutMS:     timeout.Milliseconds(),
		MaxTurns:      opts.MaxTurns,
		RunsPerQuery:  opts.Runs,
		Jobs:          opts.Jobs,
		Baseline:      opts.Baseline,
		Cases:         cases,
		ClientVersion: opts.ClientVersion,
	}
	if tier == 2 && opts.Judge.ID != "" {
		spec.Judge = &JudgeSpec{Model: opts.Judge.ID}
		if id, ok := opts.Judge.CLIModelID(preferredHarness(sel.Model)); ok {
			spec.Judge.ModelID = id
		}
	}
	if prior, err := priorEntry(set, sel.Key(), kind); err == nil && prior != nil {
		spec.PriorEntry = prior
	}
	return &plannedUnit{set: set, sel: sel, kind: kind, bundle: bundle, spec: spec}, nil
}

// runSet extracts the unit's case allowlist from the Needs computation: nil
// (run all) when no selection flag narrows it, the needed subset otherwise,
// any=false when nothing needs running.
func runSet(sc plan.SkillCatalog, sel harness.Selection, kind plan.Kind,
	need map[string]map[plan.CaseRef]bool, flags bool, evalFilter string) (cases []string, any bool) {
	perSel := need[sel.Key()]
	all := true
	for cr, needed := range perSel {
		if cr.Skill != sc.Skill || cr.Kind != kind {
			continue
		}
		any = any || needed
		if needed {
			cases = append(cases, cr.Case)
		} else {
			all = false
		}
	}
	if !flags && evalFilter == "" && all && any {
		return nil, true // no narrowing: run everything, keep the spec small
	}
	return cases, any
}

// harnessOptions renders the model's harness preference list, the Preferred
// harness first.
func harnessOptions(m model.Model) []HarnessOption {
	out := []HarnessOption{}
	if id, ok := m.CLIModelID(m.Preferred); ok {
		out = append(out, HarnessOption{Harness: m.Preferred, ModelID: id})
	}
	for _, hid := range m.SupportedHarnessIDs() {
		if hid == m.Preferred {
			continue
		}
		id, _ := m.CLIModelID(hid)
		out = append(out, HarnessOption{Harness: hid, ModelID: id})
	}
	return out
}

// preferredHarness is the harness the scheduler will most likely pick.
func preferredHarness(m model.Model) string { return m.Preferred }

// priorEntry loads the unit's existing results entry, opaque payload for
// in-pod seeding; nil when none exists.
func priorEntry(set layout.EvalSet, key string, kind plan.Kind) (json.RawMessage, error) {
	f, _, err := results.LoadDir(set.ResultsDir, set.Plugin.Name, set.Skill)
	if err != nil {
		return nil, err
	}
	if kind == plan.KindTriggers {
		if entry := f.Trigger(key); entry != nil {
			return MarshalTriggerEntry(entry)
		}
		return nil, nil
	}
	if entry := f.Eval(key); entry != nil {
		return MarshalEvalEntry(entry)
	}
	return nil, nil
}

// uploadBundles dedupes and uploads every distinct workspace.
func uploadBundles(ctx context.Context, c *Client, units []plannedUnit) error {
	uploaded := map[string]bool{}
	for i := range units {
		b := units[i].bundle
		if uploaded[b.Digest] {
			continue
		}
		uploaded[b.Digest] = true
		cached, err := c.HasWorkspace(ctx, b.Digest)
		if err != nil {
			return err
		}
		if cached {
			continue
		}
		if err := c.UploadWorkspace(ctx, b.Digest, b.Data); err != nil {
			return err
		}
	}
	return nil
}

// landUnit merges one settled unit onto the local results file and reports
// it finished; returns whether the unit counts as failed.
func landUnit(pu *plannedUnit, u UnitStatusWire, rep run.Reporter, format string) (failed bool, err error) {
	ref := plan.UnitRef{Skill: pu.set.Skill, Key: pu.sel.Key(), Kind: pu.kind}
	if u.Result == nil {
		reason := u.Reason
		if reason == "" {
			reason = "no result"
		}
		detail := u.Detail
		if detail == "" {
			detail = reason
		}
		if u.Reason == "HarnessUnavailable" {
			rep.UnitSkipped(ref, detail)
			return false, nil
		}
		if u.Reason == "WorkspaceLost" {
			return true, errors.New("workspace expired server-side; rerun to re-upload and resubmit")
		}
		return true, fmt.Errorf("failed remotely: %s", detail)
	}

	f, _, err := results.LoadDir(pu.set.ResultsDir, pu.set.Plugin.Name, pu.set.Skill)
	if err != nil {
		return true, err
	}
	if pu.kind == plan.KindTriggers {
		var entry results.TriggerEntry
		if err := json.Unmarshal(u.Result.Entry, &entry); err != nil {
			return true, fmt.Errorf("parse remote trigger entry: %w", err)
		}
		f.SetTrigger(pu.sel.Key(), &entry)
	} else {
		var entry results.EvalEntry
		if err := json.Unmarshal(u.Result.Entry, &entry); err != nil {
			return true, fmt.Errorf("parse remote eval entry: %w", err)
		}
		f.SetEval(pu.sel.Key(), &entry)
	}
	saved, err := f.SaveDir(pu.set.ResultsDir, format)
	if err != nil {
		return true, err
	}

	sum := run.UnitSummary{
		Executed: true,
		Passed:   u.Result.Summary.CasesPassed,
		Failed:   u.Result.Summary.CasesFailed,
		Errored:  u.Result.Summary.CasesErrored,
		Total:    u.Result.Summary.CasesPassed + u.Result.Summary.CasesFailed + u.Result.Summary.CasesErrored,
	}
	rep.UnitFinished(ref, sum, saved)
	return u.Result.Failed, nil
}
