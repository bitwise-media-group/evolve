// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"fmt"
	"io"
)

// Kind distinguishes the two eval tiers a unit belongs to.
type Kind int

const (
	KindTriggers Kind = iota
	KindEvals
)

// Mode is how a unit executes: a real agent run, or token counting only.
type Mode int

const (
	ModeRun Mode = iota
	ModeCountOnly
)

// Status is one item's outcome (a query for triggers, an eval for evals).
type Status int

const (
	StatusPass Status = iota
	StatusFail
	StatusSkip
	StatusError
)

// UnitRef identifies one execution unit: a (skill, provider/model, tier) triple.
type UnitRef struct {
	Skill string
	Key   string // provider.Selection.Key(): "provider/model"
	Kind  Kind
}

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
	Status  Status
	Detail  string
	Output  string
	Metrics ItemMetrics

	// WorkspacePath is the retained throwaway workspace the agent ran in, and
	// LogPath the file holding its full output; the live TUI opens them on a
	// keypress. Both are empty unless the run retains workspaces (TUI runs do).
	WorkspacePath string
	LogPath       string
}

// ItemMetrics is the per-case figures the live dashboard shows. All fields are
// optional: triggers fill hits/runs and the input-side estimate; evals fill the
// duration, measured input/output tokens, cost, and assertion tally.
type ItemMetrics struct {
	Hits                *int
	Runs                *int
	AvgRunSeconds       *float64 // triggers: avg per run; evals: executor duration
	InputTokens         *int     // fresh (uncached) input
	CacheReadTokens     *int     // evals only
	CacheCreationTokens *int     // evals only
	OutputTokens        *int     // evals only
	CostUSD             *float64
	AssertPassed        *int
	AssertTotal         *int
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
	UnitStarted(u UnitRef, total, runs int, mode Mode)
	UnitSkipped(u UnitRef, reason string)
	ItemStarted(u UnitRef, item ItemStart)
	ItemDone(u UnitRef, item ItemResult)
	UnitFinished(u UnitRef, sum UnitSummary, savedRel string)
	Warn(format string, a ...any)
}

// PlainReporter reproduces the historical line-based output exactly: it is the
// default when Options.Reporter is nil, so non-TTY runs and the engine tests
// are unaffected by the reporter indirection.
type PlainReporter struct {
	Stdout io.Writer
	Stderr io.Writer
}

func (r PlainReporter) UnitStarted(u UnitRef, total, runs int, mode Mode) {
	m := "count-only"
	if mode == ModeRun {
		m = "run"
	}
	if u.Kind == KindTriggers {
		fmt.Fprintf(r.Stdout, "\n=== %s / %s (%d queries x %d runs, %s) ===\n", u.Skill, u.Key, total, runs, m)
		return
	}
	fmt.Fprintf(r.Stdout, "\n=== %s / %s (%s) ===\n", u.Skill, u.Key, m)
}

func (r PlainReporter) UnitSkipped(u UnitRef, reason string) {
	fmt.Fprintf(r.Stdout, "\n=== %s / %s (skip: %s) ===\n", u.Skill, u.Key, reason)
}

// ItemStarted is a no-op for plain output: the historical line format reports
// items only on completion.
func (r PlainReporter) ItemStarted(UnitRef, ItemStart) {}

func (r PlainReporter) ItemDone(u UnitRef, item ItemResult) {
	if u.Kind == KindEvals {
		// A runtime-error diagnostic (the agent run produced no gradable output)
		// is a failure, not a result, so it goes to stderr; the per-assertion
		// PASS/FAIL block is normal result output and stays on stdout.
		w := r.Stdout
		if item.Status == StatusError {
			w = r.Stderr
		}
		fmt.Fprint(w, item.Detail) // pre-rendered, may span several lines
		return
	}
	fmt.Fprintf(r.Stdout, "  [%s] %s\n", marker(item.Status), item.Detail)
}

func (r PlainReporter) UnitFinished(u UnitRef, sum UnitSummary, savedRel string) {
	if sum.Executed {
		noun, extra := "queries", ""
		if u.Kind == KindEvals {
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
func marker(s Status) string {
	switch s {
	case StatusPass:
		return "PASS"
	case StatusFail:
		return "FAIL"
	case StatusSkip:
		return "SKIP"
	case StatusError:
		return "ERROR"
	}
	return "?"
}
