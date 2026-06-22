// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"fmt"
	"io"
	"sync"

	"github.com/bitwise-media-group/evolve/internal/plan"
)

// ItemStart announces that work on one item within a unit has begun. The TUI
// uses it to mark the case running and to look up its authored spec (prompt,
// assertions, files) from the catalog; the plain reporter ignores it.
type ItemStart struct {
	Index int
	Label string // trigger query or eval id
	Runs  int    // triggers: runs scheduled for this query; evals: 1
}

// ItemResult is one finished item within a unit. Detail carries the
// human-readable body: for triggers a single line (rate/avg/expect/query) the
// plain reporter prefixes with the status marker; for evals the pre-rendered
// block of per-assertion lines (or the runtime-error line), printed verbatim.
// Output is the agent's final assistant text for evals (empty for triggers and
// runtime errors); the live TUI shows it, the plain reporter ignores it.
// Metrics carries the structured figures the dashboard renders into the tree.
type ItemResult struct {
	Index   int
	Label   string // trigger query or eval id
	Status  plan.Status
	Detail  string
	Output  string
	Metrics plan.ItemMetrics

	// WorkspacePath is the retained throwaway workspace the agent ran in, and
	// LogPath the file holding its full output; the live TUI opens them on a
	// keypress. Both are empty unless the run retains workspaces (TUI runs do).
	WorkspacePath string
	LogPath       string
}

// UnitSummary is the rollup the engine reports when a unit finishes.
type UnitSummary struct {
	Executed      bool
	Passed        int
	Failed        int
	Errored       int
	Total         int
	AvgRunSeconds *float64
}

// Reporter observes a sweep's progress. The engine calls it instead of writing
// to stdout directly, so the same run can drive plain line output or a live
// TUI. Implementations must be safe for concurrent use: ItemDone and Warn are
// called from the parallel agent-run goroutines.
type Reporter interface {
	UnitStarted(u plan.UnitRef, total, runs int, mode plan.Mode)
	UnitSkipped(u plan.UnitRef, reason string)
	ItemStarted(u plan.UnitRef, item ItemStart)
	ItemDone(u plan.UnitRef, item ItemResult)
	// BaselineStarted reports that an eval's without-skill baseline run has begun.
	// A baseline runs interleaved right before the eval's own run, so the dashboard
	// can flag that row as "running its baseline first" instead of looking stalled
	// while the (invisible) without-skill agent session executes. The plain reporter
	// ignores it, like ItemStarted.
	BaselineStarted(u plan.UnitRef, item ItemStart)
	// BaselineDone reports one finished without-skill baseline eval. Baselines are
	// not tree cases (they measure the skill's absence, not the run under test), but
	// their metrics stream live so the dashboard can show a vs-baseline delta on a
	// first-ever run instead of waiting for the next one. Detail is a one-liner for
	// plain output; Metrics carries the figures the dashboard compares against.
	BaselineDone(u plan.UnitRef, item ItemResult)
	UnitFinished(u plan.UnitRef, sum UnitSummary, savedRel string)
	Warn(format string, a ...any)
}

// PlainReporter reproduces the historical line-based output exactly: it is the
// default when Options.Reporter is nil, so non-TTY runs and the engine tests
// are unaffected by the reporter indirection.
type PlainReporter struct {
	Stdout io.Writer
	Stderr io.Writer
}

// NewPlainReporter builds a PlainReporter whose writes are serialized so it meets
// the Reporter contract that implementations be safe for concurrent use: the
// parallel agent-run goroutines call ItemDone and Warn at once, and each fmt
// call emits exactly one Write, so a single mutex shared by both writers keeps
// every line atomic — even when Stdout and Stderr target the same sink.
func NewPlainReporter(stdout, stderr io.Writer) PlainReporter {
	mu := new(sync.Mutex)
	return PlainReporter{Stdout: lockedWriter{mu, stdout}, Stderr: lockedWriter{mu, stderr}}
}

// lockedWriter serializes writes to an underlying writer through a shared mutex.
type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (r PlainReporter) UnitStarted(u plan.UnitRef, total, runs int, mode plan.Mode) {
	m := "count-only"
	if mode == plan.ModeRun {
		m = "run"
	}
	if u.Kind == plan.KindTriggers {
		fmt.Fprintf(r.Stdout, "\n=== %s / %s (%d queries x %d runs, %s) ===\n", u.Skill, u.Key, total, runs, m)
		return
	}
	fmt.Fprintf(r.Stdout, "\n=== %s / %s (%s) ===\n", u.Skill, u.Key, m)
}

func (r PlainReporter) UnitSkipped(u plan.UnitRef, reason string) {
	fmt.Fprintf(r.Stdout, "\n=== %s / %s (skip: %s) ===\n", u.Skill, u.Key, reason)
}

// ItemStarted is a no-op for plain output: the historical line format reports
// items only on completion.
func (r PlainReporter) ItemStarted(plan.UnitRef, ItemStart) {}

// BaselineStarted is a no-op for plain output: like ItemStarted, the line format
// reports a baseline only when it finishes (BaselineDone).
func (r PlainReporter) BaselineStarted(plan.UnitRef, ItemStart) {}

func (r PlainReporter) ItemDone(u plan.UnitRef, item ItemResult) {
	if u.Kind == plan.KindEvals {
		// A runtime-error diagnostic (the agent run produced no gradable output)
		// is a failure, not a result, so it goes to stderr; the per-assertion
		// PASS/FAIL block is normal result output and stays on stdout.
		w := r.Stdout
		if item.Status == plan.StatusError {
			w = r.Stderr
		}
		fmt.Fprint(w, item.Detail) // pre-rendered, may span several lines
		return
	}
	fmt.Fprintf(r.Stdout, "  [%s] %s\n", marker(item.Status), item.Detail)
}

// BaselineDone prints a concise one-line baseline result; the full grading block
// belongs to the with-skill run, not its baseline.
func (r PlainReporter) BaselineDone(u plan.UnitRef, item ItemResult) {
	fmt.Fprintf(r.Stdout, "  [base %s] %s\n", marker(item.Status), item.Detail)
}

func (r PlainReporter) UnitFinished(u plan.UnitRef, sum UnitSummary, savedRel string) {
	if sum.Executed {
		noun, extra := "queries", ""
		if u.Kind == plan.KindEvals {
			noun = "evals"
			if sum.Errored > 0 {
				extra = fmt.Sprintf(", %d errored", sum.Errored)
			}
		}
		fmt.Fprintf(r.Stdout, "  %d/%d %s passed%s%s\n",
			sum.Passed, sum.Total, noun, extra, avgSuffix(sum.AvgRunSeconds))
	}
	fmt.Fprintf(r.Stdout, "  -> %s\n", savedRel)
}

func (r PlainReporter) Warn(format string, a ...any) {
	fmt.Fprintf(r.Stderr, format, a...)
}

// marker maps a status to its plain-output token.
func marker(s plan.Status) string {
	switch s {
	case plan.StatusPass:
		return "PASS"
	case plan.StatusFail:
		return "FAIL"
	case plan.StatusSkip:
		return "SKIP"
	case plan.StatusError:
		return "ERROR"
	}
	return "?"
}
