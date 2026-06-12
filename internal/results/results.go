// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package results

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
)

// Schema is the current results.json schema version.
const Schema = 1

// File is one evals/<skill>/results.json.
type File struct {
	Schema   int                      `json:"schema"`
	Plugin   string                   `json:"plugin"`
	Skill    string                   `json:"skill"`
	Triggers map[string]*TriggerEntry `json:"triggers,omitempty"`
	Cases    map[string]*CaseEntry    `json:"cases,omitempty"`
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
	Total         int       `json:"total"`
	AvgRunSeconds *float64  `json:"avg_run_seconds,omitempty"`
	Estimate      *Estimate `json:"estimate,omitempty"`
}

// CaseEntry is one model's behavioral sweep over a skill.
type CaseEntry struct {
	Header
	Results []CaseResult `json:"results"`
	Summary CaseSummary  `json:"summary"`
}

// GradedAssertion is an authored assertion plus its verdict. Passed is
// tri-state: nil means skipped (e.g. a required binary is not installed).
type GradedAssertion struct {
	evalspec.Assertion
	Passed   *bool  `json:"passed"`
	Evidence string `json:"evidence"`
}

// CaseResult is one case's outcome.
type CaseResult struct {
	ID         string            `json:"id"`
	Passed     *bool             `json:"passed,omitempty"`
	RunSeconds *float64          `json:"run_seconds,omitempty"`
	Estimate   *Estimate         `json:"estimate,omitempty"`
	Measured   *Measured         `json:"measured,omitempty"`
	Assertions []GradedAssertion `json:"assertions,omitempty"`
}

// CaseSummary aggregates a case entry.
type CaseSummary struct {
	Passed        *int      `json:"passed,omitempty"`
	Total         int       `json:"total"`
	AvgRunSeconds *float64  `json:"avg_run_seconds,omitempty"`
	Estimate      *Estimate `json:"estimate,omitempty"`
	Measured      *Measured `json:"measured,omitempty"`
}

// Load reads the results file at path, or initialises a fresh one when the
// file is missing, unparseable, or has a different schema (clean break from
// the Python harness's formats).
func Load(path, plugin, skill string) *File {
	fresh := &File{Schema: Schema, Plugin: plugin, Skill: skill}
	data, err := os.ReadFile(path)
	if err != nil {
		return fresh
	}
	var f File
	if json.Unmarshal(data, &f) != nil || f.Schema != Schema {
		return fresh
	}
	f.Plugin, f.Skill = plugin, skill
	return &f
}

// Save writes the file atomically with deterministic formatting.
func (f *File) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SetTrigger stores entry under the model key, creating the section.
func (f *File) SetTrigger(key string, entry *TriggerEntry) {
	if f.Triggers == nil {
		f.Triggers = map[string]*TriggerEntry{}
	}
	f.Triggers[key] = entry
}

// SetCase stores entry under the model key, creating the section.
func (f *File) SetCase(key string, entry *CaseEntry) {
	if f.Cases == nil {
		f.Cases = map[string]*CaseEntry{}
	}
	f.Cases[key] = entry
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
