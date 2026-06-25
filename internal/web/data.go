// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package web

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/results"
)

// Dataset is the read-only payload the browser loads (GET /api/results) and the
// snapshot export inlines. It is a flat list of case rows plus light metadata;
// the SPA derives the facet option lists and the per-model rollup from Rows, so
// there is a single source of truth and no server-side aggregation to drift.
type Dataset struct {
	GeneratedAt string `json:"generatedAt"` // RFC3339 UTC, when the dataset was assembled
	ToolVersion string `json:"toolVersion"`
	Repo        string `json:"repo"`     // repository directory name, for the page title
	RepoPath    string `json:"repoPath"` // absolute repository root
	Rows        []Row  `json:"rows"`
}

// Row is one trigger query or eval case for one model under one skill: the unit
// the table renders and every facet filters on. Pointer metrics are omitted when
// a figure was not measured, so the UI can render "—" rather than a false zero.
type Row struct {
	Plugin   string `json:"plugin"`
	Skill    string `json:"skill"`
	Provider string `json:"provider"`          // e.g. "anthropic"
	Model    string `json:"model"`             // bare id, e.g. "claude-sonnet-4-6"
	ModelKey string `json:"modelKey"`          // "provider/model-id"
	Display  string `json:"display,omitempty"` // human-readable model name
	Harness  string `json:"harness,omitempty"` // driving CLI (claude, codex, …)
	Type     string `json:"type"`              // "trigger" | "eval"
	ID       string `json:"id"`                // eval id, or the trigger query text
	Name     string `json:"name,omitempty"`    // eval display name (empty for triggers)
	Status   string `json:"status"`            // "pass" | "fail" | "error"
	Executed bool   `json:"executed"`          // whether the entry's run completed
	RanAt    string `json:"ranAt,omitempty"`   // entry's RFC3339 run timestamp

	// ShouldTrigger is the trigger's expected behaviour (nil for eval rows).
	ShouldTrigger *bool `json:"shouldTrigger,omitempty"`
	// Hits / Runs are the trigger tally (nil for eval rows).
	Hits *int `json:"hits,omitempty"`
	Runs *int `json:"runs,omitempty"`

	DurationSeconds *float64 `json:"durationSeconds,omitempty"`
	CostUSD         *float64 `json:"costUSD,omitempty"`
	InputTokens     *int     `json:"inputTokens,omitempty"`
	OutputTokens    *int     `json:"outputTokens,omitempty"`
}

// Status values for Row.Status.
const (
	statusPass  = "pass"
	statusFail  = "fail"
	statusError = "error"
)

// Type values for Row.Type.
const (
	typeTrigger = "trigger"
	typeEval    = "eval"
)

// BuildDataset loads every committed results file under repo and flattens it
// into the read-only [Dataset] the viewer serves. It mirrors the report
// package's load walk (skipping skills with no results file) so the viewer and
// the Markdown report draw from exactly the same data.
func BuildDataset(repo *layout.Repo, toolVersion string) (*Dataset, error) {
	sets, err := repo.EvalSets()
	if err != nil {
		return nil, err
	}
	ds := &Dataset{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		ToolVersion: toolVersion,
		Repo:        filepath.Base(repo.Root),
		RepoPath:    repo.Root,
	}
	for _, set := range sets {
		if results.Find(set.ResultsDir) == "" {
			continue
		}
		f, _ := results.LoadDir(set.ResultsDir, set.Plugin.Name, set.Skill)
		for _, key := range f.ModelKeys() {
			me := f.Models[key]
			if me == nil {
				continue
			}
			if me.Triggers != nil {
				ds.Rows = append(ds.Rows, triggerRows(f, key, me.Triggers)...)
			}
			if me.Evals != nil {
				ds.Rows = append(ds.Rows, evalRows(f, key, me.Evals)...)
			}
		}
	}
	return ds, nil
}

// triggerRows flattens one model's trigger entry into table rows.
func triggerRows(f *results.File, key string, e *results.TriggerEntry) []Row {
	provider, mdl := providerModel(key, e.Header)
	rows := make([]Row, 0, len(e.Results))
	for _, r := range e.Results {
		row := Row{
			Plugin:          f.Plugin,
			Skill:           f.Skill,
			Provider:        provider,
			Model:           mdl,
			ModelKey:        key,
			Display:         e.Display,
			Harness:         e.Harness,
			Type:            typeTrigger,
			ID:              r.Query,
			Status:          triggerStatus(r),
			Executed:        e.Executed,
			RanAt:           e.RanAt,
			ShouldTrigger:   &r.ShouldTrigger,
			Hits:            r.Hits,
			Runs:            r.Runs,
			DurationSeconds: r.AvgRunSeconds,
		}
		if r.Estimate != nil {
			tokens := r.Estimate.InputTokens
			row.InputTokens = &tokens
			row.CostUSD = r.Estimate.InputCostUSD
		}
		rows = append(rows, row)
	}
	return rows
}

// evalRows flattens one model's eval entry into table rows.
func evalRows(f *results.File, key string, e *results.EvalEntry) []Row {
	provider, mdl := providerModel(key, e.Header)
	rows := make([]Row, 0, len(e.Results))
	for _, r := range e.Results {
		row := Row{
			Plugin:          f.Plugin,
			Skill:           f.Skill,
			Provider:        provider,
			Model:           mdl,
			ModelKey:        key,
			Display:         e.Display,
			Harness:         e.Harness,
			Type:            typeEval,
			ID:              r.ID,
			Name:            r.Name,
			Status:          evalStatus(r),
			Executed:        e.Executed,
			RanAt:           e.RanAt,
			DurationSeconds: r.RunSeconds(),
		}
		row.CostUSD, row.InputTokens, row.OutputTokens = evalMetrics(r)
		rows = append(rows, row)
	}
	return rows
}

// providerModel resolves a row's provider and bare model id, preferring the
// entry header (authoritative) and falling back to splitting the "provider/id"
// model key for entries written before the header carried them.
func providerModel(key string, h results.Header) (provider, mdl string) {
	provider, mdl = h.Provider, h.Model
	if provider != "" && mdl != "" {
		return provider, mdl
	}
	if p, id, ok := strings.Cut(key, "/"); ok {
		if provider == "" {
			provider = p
		}
		if mdl == "" {
			mdl = id
		}
	} else if mdl == "" {
		mdl = key
	}
	return provider, mdl
}

// triggerStatus maps a trigger result to a row status. A query passes when it
// behaved as expected (the engine sets Passed); a nil verdict means the query
// never produced a usable tally.
func triggerStatus(r results.TriggerResult) string {
	switch {
	case r.Passed == nil:
		return statusError
	case *r.Passed:
		return statusPass
	default:
		return statusFail
	}
}

// evalStatus maps an eval result to a row status: a runtime error (agent
// produced no usable output) or a nil verdict is an error, otherwise pass/fail.
func evalStatus(r results.EvalResult) string {
	switch {
	case r.RuntimeError != "":
		return statusError
	case r.Passed == nil:
		return statusError
	case *r.Passed:
		return statusPass
	default:
		return statusFail
	}
}

// evalMetrics picks an eval's cost and token figures, preferring the measured
// session usage and falling back to the counting-API estimate for input/cost.
func evalMetrics(r results.EvalResult) (cost *float64, inTok, outTok *int) {
	if r.Measured != nil {
		cost = r.Measured.CostUSD
		inTok = r.Measured.InputTokens
		outTok = r.Measured.OutputTokens
	}
	if r.Estimate != nil {
		if inTok == nil {
			tokens := r.Estimate.InputTokens
			inTok = &tokens
		}
		if cost == nil {
			cost = r.Estimate.InputCostUSD
		}
	}
	return cost, inTok, outTok
}
