// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/bitwise-media-group/evolve/internal/cli"
	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/run"
	"github.com/bitwise-media-group/evolve/internal/telemetry"
	"github.com/bitwise-media-group/evolve/internal/tui"
)

// interactiveTUI reports whether the interactive TUI should run: stdout is a
// real terminal and the user has not opted out via --no-tui or EVOLVE_NO_TUI.
func interactiveTUI(cmd *cobra.Command, noTUI bool) bool {
	if noTUI || os.Getenv("EVOLVE_NO_TUI") != "" {
		return false
	}
	f, ok := cmd.OutOrStdout().(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// switchWriter is an io.Writer whose destination can be repointed at runtime —
// used so the token counter's diagnostics start discarded and are then routed
// into the TUI once the program exists.
type switchWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func newSwitchWriter(w io.Writer) *switchWriter { return &switchWriter{w: w} }

func (s *switchWriter) Write(b []byte) (int, error) {
	s.mu.Lock()
	w := s.w
	s.mu.Unlock()
	if w == nil {
		return len(b), nil
	}
	return w.Write(b)
}

func (s *switchWriter) set(w io.Writer) {
	s.mu.Lock()
	s.w = w
	s.mu.Unlock()
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }

// forward returns a writer that turns each token-counter line into a TUI warning.
func forward(rep run.Reporter) io.Writer {
	return writerFunc(func(b []byte) (int, error) {
		rep.Warn("%s", string(b))
		return len(b), nil
	})
}

// runWithUI drives the full-screen program: it shows the selection form, then
// runs engine via the supplied callback once the user chooses RUN, keeping the
// dashboard interactive until they quit. The session owns the form's selection
// state; the callback receives the chosen RunRequest and the reporter to attach
// to its run.Options.
func runWithUI(cmd *cobra.Command, session *plan.Session, cat []plan.SkillCatalog, evalFilter string,
	prior plan.PriorMetrics, thresholds tui.Thresholds,
	engine func(ctx context.Context, req tui.RunRequest, rep run.Reporter) (bool, error),
) (bool, error) {
	runReq := make(chan tui.RunRequest, 1)
	model := tui.New(session, cat, evalFilter, prior, thresholds, runReq)
	p := tea.NewProgram(model)
	rep := tui.NewReporter(p)

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	var (
		engFailed bool
		engErr    error
	)
	engineDone := make(chan struct{})
	progExited := make(chan struct{})
	go func() {
		defer close(engineDone)
		select {
		case req, ok := <-runReq:
			if !ok {
				return
			}
			engFailed, engErr = engine(ctx, req, rep)
			p.Send(tui.RunDone(engFailed, engErr))
		case <-progExited:
		}
	}()

	if _, err := p.Run(); err != nil {
		return false, err
	}
	close(progExited) // unblock the goroutine if the user cancelled before running
	cancel()          // stop the engine if the user quit mid-run
	<-engineDone

	if errors.Is(engErr, context.Canceled) {
		engErr = nil // a user-initiated quit is not an error
	}
	return engFailed, engErr
}

// runSub runs a sibling subcommand (checks/report) inline, folding an
// ErrFailures into the failures flag rather than aborting.
func runSub(cmd, sub *cobra.Command, failures *bool) error {
	sub.SetContext(cmd.Context())
	if err := forwardFlags(cmd.Flags(), sub.Flags()); err != nil {
		return err
	}
	if err := sub.RunE(sub, nil); err != nil {
		if errors.Is(err, cli.ErrFailures) {
			*failures = true
			return nil
		}
		return err
	}
	return nil
}

// uiRun is the single interactive path shared by `run triggers`, `run evals`,
// and `run all`. The form always spans both tiers; def decides which tier is
// checked by default, and the config/CLI filters (--plugin/--skill/--eval/--new/--failed)
// refine the initial selection. withChecksReport adds `run all`'s static-checks step
// before and report step after.
func uiRun(cmd *cobra.Command, sweep *SweepFlags, def plan.Tiers,
	triggerRuns int, evalFilter, judgeModel, failMsg string, withChecksReport bool) error {
	var failures bool
	if withChecksReport {
		if err := runSub(cmd, checksCmd, &failures); err != nil {
			return err
		}
	}

	counterOut := newSwitchWriter(io.Discard)
	common, err := sweep.sweepOptionsW(cmd, counterOut)
	if err != nil {
		return err
	}
	cat, err := run.Catalog(common)
	if err != nil {
		return err
	}

	// Retain every agent workspace under one run-scoped root so the dashboard can
	// open a finished execution's workspace and output log; remove it when the TUI
	// session ends, unless --keep-workspaces leaves it behind for inspection.
	retainRoot, err := os.MkdirTemp("", "evolve-run.")
	if err != nil {
		return err
	}
	common.RetainRoot = retainRoot
	defer func() {
		if !common.KeepWorkspaces {
			_ = os.RemoveAll(retainRoot)
		}
	}()

	// Build the form's session: it lists every configured harness and model, seeds
	// the new/modified/failed filters and the enabled harnesses/models from the
	// flag-resolved run, and holds the per-(model,case) reasons the filters act on.
	// The same Session resolves the plan the engine runs, so the form, dashboard,
	// and engine cannot drift.
	allSels, err := opts.RunnableSelections("all", "")
	if err != nil {
		return err
	}
	reasons := run.CaseReasons(common, cat, allSels, def, evalFilter)
	models, err := opts.ConfiguredModels()
	if err != nil {
		return err
	}
	harnesses, err := opts.Harnesses()
	if err != nil {
		return err
	}
	var hstates []plan.HarnessState
	for _, h := range harnesses {
		_, avail := harness.Available(h)
		hstates = append(hstates, plan.HarnessState{Harness: h, Available: avail})
	}
	eligible, err := opts.EligibleHarnesses(sweep.Harness)
	if err != nil {
		return err
	}
	var eligibleIDs []string
	for _, h := range eligible {
		eligibleIDs = append(eligibleIDs, h.ID())
	}
	var enabledKeys []string
	for _, sel := range common.Selected {
		enabledKeys = append(enabledKeys, sel.Key())
	}
	filters := plan.Filters{New: sweep.NewOnly, Modified: sweep.ModifiedOnly, Failed: sweep.FailedOnly}

	// Seed the dashboard with the last committed metrics so it can color deltas as
	// cases finish — the live run is compared against the run it replaces.
	prior := plan.LoadPriorMetrics(cat)
	session := plan.NewSession(cat, models, hstates, prior, reasons, filters, eligibleIDs, enabledKeys)

	// Per-tier timeouts: the triggers/evals defaults (120/600) unless the user
	// set --timeout explicitly, in which case it applies to both.
	triggerTO, evalTO := perTierTimeouts(cmd, sweep.Timeout)

	engine := func(ctx context.Context, req tui.RunRequest, rep run.Reporter) (bool, error) {
		counterOut.set(forward(rep))
		base := common
		// Wrap the live TUI reporter so telemetry records metrics and structured
		// logs from the same events; a no-op when telemetry is disabled.
		base.Reporter = telemetry.WrapReporter(rep)
		base.Selected = req.Models
		// The per-model filters already encode the plugin/skill narrowing.
		base.PluginFilter = nil
		base.SkillFilter = nil
		// The form's Selection already encodes --new/--failed/--modified, so clear
		// those flags or the engine would re-filter the user's selection.
		base = base.ClearSelectionFlags()

		// Resolve the form's Selection through the shared planner — the same
		// plan.Build the form previewed with — and execute its per-model filters, so
		// what runs is exactly what the form showed.
		p := plan.Build(cat, req.Models, req.Selection, prior)

		// One interleaved sweep: per skill, each model runs its triggers then its
		// evals before the next. Per-model filters skip a model whose results are
		// already complete and rerun a stale one — matching --new.
		failed, err := run.Sweep(ctx, run.SweepOptions{
			Options:        base,
			Tiers:          plan.Tiers{Triggers: true, Evals: true},
			Runs:           triggerRuns,
			EvalFilter:     evalFilter,
			JudgeModel:     judgeModel,
			TriggerTimeout: triggerTO,
			EvalTimeout:    evalTO,
			Filters:        p.Filters(),
		})
		if err != nil {
			_ = saveCounter(common.Counter)
			return failed, err
		}
		if e := saveCounter(common.Counter); e != nil {
			return failed, e
		}
		if !withChecksReport {
			if e := opts.RegenerateReports(); e != nil {
				return failed, e
			}
		}
		return failed, nil
	}

	// The dashboard classifies its rollup glyphs against the same report
	// thresholds `report --check` gates on, so the two verdicts cannot drift.
	th := opts.Thresholds()
	thresholds := tui.Thresholds{Triggers: th.TriggersMinPassRate, Evals: th.EvalsMinPassRate}

	failed, err := runWithUI(cmd, session, cat, evalFilter, prior, thresholds, engine)
	if err != nil {
		return err
	}
	failures = failures || failed

	if withChecksReport {
		if err := runSub(cmd, reportCmd, &failures); err != nil {
			return err
		}
	}
	if failures {
		return failOrWarn(cmd, "%s", failMsg)
	}
	return nil
}
