// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package results

import (
	"math"
	"os"
	"path/filepath"

	"github.com/bitwise-media-group/evolve/internal/encfmt"
	"github.com/bitwise-media-group/evolve/internal/evalspec"
)

// Schema is the current results-file schema version. v2 made the per-eval
// result a superset of skill-creator's grading.json (expectations with
// text/passed/evidence, summary, timing) and renamed the cases section.
//
// The runtime-error fields (EvalResult.RuntimeError, EvalSummary.Errored) are
// additive and omitempty, so they do not change the schema number: old files
// load unchanged, and an older binary simply ignores them. They do shift the
// meaning of the failed count — an eval whose agent never produced usable
// output is now reported as errored rather than failed.
const Schema = 2

// File is one evals/<skill>/results.<ext>.
type File struct {
	Schema   int                      `json:"schema"`
	Plugin   string                   `json:"plugin"`
	Skill    string                   `json:"skill"`
	Triggers map[string]*TriggerEntry `json:"triggers,omitempty"`
	Evals    map[string]*EvalEntry    `json:"evals,omitempty"`
}

// Header is the run metadata common to both entry kinds.
type Header struct {
	Provider       string   `json:"provider"`
	Model          string   `json:"model"`
	Display        string   `json:"display"`
	ToolVersion    string   `json:"tool_version"`
	RanAt          string   `json:"ran_at"` // RFC3339 UTC, second precision
	Executed       bool     `json:"executed"`
	RunsPerQuery   int      `json:"runs_per_query,omitempty"` // triggers only
	TimeoutSeconds int      `json:"timeout_seconds"`
	Pricing        *Pricing `json:"pricing"` // explicit null when unpriced
}

// Pricing snapshots the model's USD-per-MTok rates at run time.
type Pricing struct {
	InputPerMTok  *float64 `json:"input_per_mtok"`
	OutputPerMTok *float64 `json:"output_per_mtok"`
}

// Estimate is the counting-API figure for SKILL.md + prompt — the marginal
// context a triggering eval loads — priced at the model's input rate.
type Estimate struct {
	InputTokens  int      `json:"input_tokens"`
	InputCostUSD *float64 `json:"input_cost_usd,omitempty"`
}

// Measured is the harness-reported usage of a live case session. Input
// includes cache writes and reads.
type Measured struct {
	InputTokens  *int     `json:"input_tokens,omitempty"`
	OutputTokens *int     `json:"output_tokens,omitempty"`
	CostUSD      *float64 `json:"cost_usd,omitempty"`
}

// TriggerEntry is one model's trigger sweep over a skill.
type TriggerEntry struct {
	Header
	Results []TriggerResult `json:"results"`
	Summary TriggerSummary  `json:"summary"`
}

// TriggerResult is one query's outcome. Hits/Runs are exact integers (the
// rate and the 0.5 pass threshold are derived at render time).
type TriggerResult struct {
	Query         string    `json:"query"`
	ShouldTrigger bool      `json:"should_trigger"`
	Hits          *int      `json:"hits,omitempty"`
	Runs          *int      `json:"runs,omitempty"`
	Passed        *bool     `json:"passed,omitempty"`
	AvgRunSeconds *float64  `json:"avg_run_seconds,omitempty"`
	Estimate      *Estimate `json:"estimate,omitempty"`
}

// TriggerSummary aggregates a trigger entry.
type TriggerSummary struct {
	Passed        *int      `json:"passed,omitempty"`
	Failed        *int      `json:"failed,omitempty"`
	Total         int       `json:"total"`
	PassRate      *float64  `json:"pass_rate,omitempty"`
	AvgRunSeconds *float64  `json:"avg_run_seconds,omitempty"`
	Estimate      *Estimate `json:"estimate,omitempty"`
}

// EvalEntry is one model's behavioral sweep over a skill.
type EvalEntry struct {
	Header
	Results []EvalResult `json:"results"`
	Summary EvalSummary  `json:"summary"`
}

// GradedAssertion is one graded expectation: skill-creator's grading.json
// entry shape (text, passed, evidence) plus the authored assertion echoed
// for full fidelity. Text shadows the embedded assertion's authored text —
// llm entries carry it verbatim, deterministic checks a derived statement.
// Passed is tri-state: nil means skipped (e.g. a required binary is not
// installed). Source records which authored field produced the entry.
type GradedAssertion struct {
	evalspec.Assertion
	Text     string `json:"text"`
	Passed   *bool  `json:"passed"`
	Evidence string `json:"evidence"`
	Source   string `json:"source,omitempty"` // "expectation" or "assertion"
}

// GradeSummary aggregates one eval's graded expectations with grading.json's
// field names. PassRate excludes skips — passed/(passed+failed), identical
// to passed/total when nothing was skipped — and is omitted when nothing
// executed.
type GradeSummary struct {
	Passed   int      `json:"passed"`
	Failed   int      `json:"failed"`
	Total    int      `json:"total"`
	PassRate *float64 `json:"pass_rate,omitempty"`
	Skipped  int      `json:"skipped,omitempty"`
}

// Timing mirrors skill-creator's timing.json field names. Wave 1 populates
// the executor duration (the agent run; grading excluded) and the measured
// token total; the grader fields await grading instrumentation.
type Timing struct {
	TotalTokens             *int     `json:"total_tokens,omitempty"`
	DurationMS              *int     `json:"duration_ms,omitempty"`
	TotalDurationSeconds    *float64 `json:"total_duration_seconds,omitempty"`
	ExecutorStart           string   `json:"executor_start,omitempty"`
	ExecutorEnd             string   `json:"executor_end,omitempty"`
	ExecutorDurationSeconds *float64 `json:"executor_duration_seconds,omitempty"`
	GraderStart             string   `json:"grader_start,omitempty"`
	GraderEnd               string   `json:"grader_end,omitempty"`
	GraderDurationSeconds   *float64 `json:"grader_duration_seconds,omitempty"`
}

// ExecutionMetrics mirrors skill-creator's metrics.json field names;
// population arrives with transcript instrumentation (wave 2).
type ExecutionMetrics struct {
	ToolCalls         map[string]int `json:"tool_calls,omitempty"`
	TotalToolCalls    *int           `json:"total_tool_calls,omitempty"`
	TotalSteps        *int           `json:"total_steps,omitempty"`
	FilesCreated      []string       `json:"files_created,omitempty"`
	ErrorsEncountered *int           `json:"errors_encountered,omitempty"`
	OutputChars       *int           `json:"output_chars,omitempty"`
	TranscriptChars   *int           `json:"transcript_chars,omitempty"`
}

// EvalResult is one eval's outcome — a superset of a skill-creator
// grading.json document, plus evolve's identity, token, and cost extras.
type EvalResult struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// RuntimeError is a non-empty reason when the agent run produced no usable
	// output (auth blocked, crash, error envelope) — a runtime failure as
	// opposed to an eval that ran but failed assertions. When set, Passed is
	// nil: the eval neither passed nor failed.
	RuntimeError     string            `json:"runtime_error,omitempty"`
	Passed           *bool             `json:"passed,omitempty"`
	Estimate         *Estimate         `json:"estimate,omitempty"`
	Measured         *Measured         `json:"measured,omitempty"`
	Expectations     []GradedAssertion `json:"expectations,omitempty"`
	Summary          *GradeSummary     `json:"summary,omitempty"`
	ExecutionMetrics *ExecutionMetrics `json:"execution_metrics,omitempty"`
	Timing           *Timing           `json:"timing,omitempty"`
}

// EvalSummary aggregates an eval entry. Errored counts evals whose agent run
// failed to produce usable output (see EvalResult.RuntimeError); they are
// excluded from PassRate, like skips.
type EvalSummary struct {
	Passed        *int      `json:"passed,omitempty"`
	Failed        *int      `json:"failed,omitempty"`
	Errored       *int      `json:"errored,omitempty"`
	Total         int       `json:"total"`
	PassRate      *float64  `json:"pass_rate,omitempty"`
	AvgRunSeconds *float64  `json:"avg_run_seconds,omitempty"`
	Estimate      *Estimate `json:"estimate,omitempty"`
	Measured      *Measured `json:"measured,omitempty"`
}

// generatedComment heads results files in the formats that carry comments.
const generatedComment = "Maintained by evolve run; do not edit by hand."

// Find returns the path of the existing results file in dir, probing the
// supported extensions in discovery order, or "" when none exists.
func Find(dir string) string {
	for _, ext := range encfmt.Extensions {
		path := filepath.Join(dir, "results."+ext)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

// LoadDir finds and decodes the results file in dir regardless of format,
// or initialises a fresh one when the file is missing, unparseable, or has
// a different schema (clean break). reset reports that an existing file was
// discarded, so callers can tell the user history is starting over.
func LoadDir(dir, plugin, skill string) (f *File, reset bool) {
	fresh := &File{Schema: Schema, Plugin: plugin, Skill: skill}
	path := Find(dir)
	if path == "" {
		return fresh, false
	}
	var loaded File
	if encfmt.DecodeFile(path, &loaded) != nil || loaded.Schema != Schema {
		return fresh, true
	}
	loaded.Plugin, loaded.Skill = plugin, skill
	return &loaded, false
}

// SaveDir writes results.<format> atomically with deterministic formatting,
// then removes stale results siblings left by a format switch. It returns
// the written path.
func (f *File) SaveDir(dir, format string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	format = encfmt.Canonical(format)
	if format == "" {
		format = "json"
	}
	data, err := encfmt.Marshal(f, format, generatedComment)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "results."+format)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	for _, ext := range encfmt.Extensions {
		if stale := filepath.Join(dir, "results."+ext); stale != path {
			_ = os.Remove(stale)
		}
	}
	return path, nil
}

// SetTrigger stores entry under the model key, creating the section.
func (f *File) SetTrigger(key string, entry *TriggerEntry) {
	if f.Triggers == nil {
		f.Triggers = map[string]*TriggerEntry{}
	}
	f.Triggers[key] = entry
}

// SetEval stores entry under the model key, creating the section.
func (f *File) SetEval(key string, entry *EvalEntry) {
	if f.Evals == nil {
		f.Evals = map[string]*EvalEntry{}
	}
	f.Evals[key] = entry
}

// Round1 rounds to 1 decimal (seconds), Round6 to 6 (costs) — always round
// before marshaling so committed files never carry float noise.
func Round1(x float64) float64 { return math.Round(x*10) / 10 }

// Round6 rounds to 6 decimals.
func Round6(x float64) float64 { return math.Round(x*1e6) / 1e6 }

// PricingOf snapshots a model's rates, or nil (serialized as an explicit
// null) when the model has no published pricing.
func PricingOf(inputPerMTok, outputPerMTok *float64) *Pricing {
	if inputPerMTok == nil && outputPerMTok == nil {
		return nil
	}
	return &Pricing{InputPerMTok: inputPerMTok, OutputPerMTok: outputPerMTok}
}

// NewEstimate builds an estimate from a token count and the model's input
// rate; nil when no count is available.
func NewEstimate(tokens *int, inputPerMTok *float64) *Estimate {
	if tokens == nil {
		return nil
	}
	e := &Estimate{InputTokens: *tokens}
	if inputPerMTok != nil {
		cost := Round6(float64(*tokens) / 1e6 * *inputPerMTok)
		e.InputCostUSD = &cost
	}
	return e
}

// SummarizeExpectations tallies one eval's graded expectations into the
// grading.json summary shape.
func SummarizeExpectations(graded []GradedAssertion) *GradeSummary {
	s := &GradeSummary{Total: len(graded)}
	for _, g := range graded {
		switch {
		case g.Passed == nil:
			s.Skipped++
		case *g.Passed:
			s.Passed++
		default:
			s.Failed++
		}
	}
	if executed := s.Passed + s.Failed; executed > 0 {
		rate := Round6(float64(s.Passed) / float64(executed))
		s.PassRate = &rate
	}
	return s
}

// SumEstimates totals per-result estimates; nil when none exist. The cost
// total is present only when at least one estimate carries a cost.
func SumEstimates(estimates []*Estimate) *Estimate {
	var tokens int
	var cost float64
	var hasTokens, hasCost bool
	for _, e := range estimates {
		if e == nil {
			continue
		}
		tokens += e.InputTokens
		hasTokens = true
		if e.InputCostUSD != nil {
			cost += *e.InputCostUSD
			hasCost = true
		}
	}
	if !hasTokens {
		return nil
	}
	sum := &Estimate{InputTokens: tokens}
	if hasCost {
		rounded := Round6(cost)
		sum.InputCostUSD = &rounded
	}
	return sum
}
