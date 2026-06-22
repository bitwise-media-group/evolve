// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package plan

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

// CaseRef identifies one authored case (a trigger query or eval id) within a
// tier, independent of any model. It is the key the selection form and the
// per-case run matrix share.
type CaseRef struct {
	Skill string
	Kind  Kind
	Case  string // trigger query or eval id
}

// Tiers selects which eval tiers a plan covers: a single-tier command enables
// one, `run all` enables both.
type Tiers struct {
	Triggers bool
	Evals    bool
}
