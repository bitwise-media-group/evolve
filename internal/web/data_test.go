// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package web

import (
	"path/filepath"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/results"
)

func TestBuildDataset(t *testing.T) {
	repo, _ := fixtureRepo(t)
	ds, err := BuildDataset(repo, "v9.9.9")
	if err != nil {
		t.Fatalf("BuildDataset: %v", err)
	}
	if ds.ToolVersion != "v9.9.9" {
		t.Errorf("ToolVersion = %q, want v9.9.9", ds.ToolVersion)
	}
	if ds.GeneratedAt == "" {
		t.Error("GeneratedAt is empty")
	}
	if len(ds.Rows) != 4 {
		t.Fatalf("got %d rows, want 4 (2 triggers + 2 evals)", len(ds.Rows))
	}
	wantPlugin := repo.Plugins[0].Name
	for _, r := range ds.Rows {
		if r.Plugin != wantPlugin {
			t.Errorf("row %s: Plugin = %q, want %q", r.ID, r.Plugin, wantPlugin)
		}
		if r.Skill != "greeter" {
			t.Errorf("row %s: Skill = %q, want greeter", r.ID, r.Skill)
		}
		if r.Provider != "anthropic" || r.Model != "claude-sonnet-4-6" {
			t.Errorf("row %s: provider/model = %q/%q", r.ID, r.Provider, r.Model)
		}
		if r.ModelKey != "anthropic/claude-sonnet-4-6" {
			t.Errorf("row %s: ModelKey = %q", r.ID, r.ModelKey)
		}
	}

	// Trigger pass row.
	greet := rowByID(t, ds.Rows, "please greet me")
	if greet.Type != typeTrigger || greet.Status != statusPass {
		t.Errorf("greet: type/status = %q/%q, want trigger/pass", greet.Type, greet.Status)
	}
	if greet.Hits == nil || *greet.Hits != 3 || greet.Runs == nil || *greet.Runs != 3 {
		t.Errorf("greet: hits/runs = %v/%v, want 3/3", greet.Hits, greet.Runs)
	}
	if greet.InputTokens == nil || *greet.InputTokens != 1200 {
		t.Errorf("greet: InputTokens = %v, want 1200", greet.InputTokens)
	}
	if greet.CostUSD == nil || *greet.CostUSD != 0.004 {
		t.Errorf("greet: CostUSD = %v, want 0.004", greet.CostUSD)
	}
	if greet.ShouldTrigger == nil || !*greet.ShouldTrigger {
		t.Errorf("greet: ShouldTrigger = %v, want true", greet.ShouldTrigger)
	}

	// Trigger fail row.
	weather := rowByID(t, ds.Rows, "what is the weather")
	if weather.Status != statusFail {
		t.Errorf("weather: status = %q, want fail", weather.Status)
	}

	// Eval pass row, with measured metrics.
	polite := rowByID(t, ds.Rows, "greets-politely")
	if polite.Type != typeEval || polite.Status != statusPass {
		t.Errorf("polite: type/status = %q/%q, want eval/pass", polite.Type, polite.Status)
	}
	if polite.Name != "greets politely" {
		t.Errorf("polite: Name = %q", polite.Name)
	}
	if polite.DurationSeconds == nil || *polite.DurationSeconds != 4.5 {
		t.Errorf("polite: DurationSeconds = %v, want 4.5", polite.DurationSeconds)
	}
	if polite.CostUSD == nil || *polite.CostUSD != 0.01 {
		t.Errorf("polite: CostUSD = %v, want 0.01", polite.CostUSD)
	}
	if polite.InputTokens == nil || *polite.InputTokens != 1000 {
		t.Errorf("polite: InputTokens = %v, want 1000", polite.InputTokens)
	}
	if polite.OutputTokens == nil || *polite.OutputTokens != 200 {
		t.Errorf("polite: OutputTokens = %v, want 200", polite.OutputTokens)
	}

	// Eval runtime-error row.
	empty := rowByID(t, ds.Rows, "handles-empty")
	if empty.Status != statusError {
		t.Errorf("empty: status = %q, want error", empty.Status)
	}
}

func TestBuildDatasetNoResults(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root+"/.claude-plugin/plugin.json", `{"name":"demo"}`)
	repo, err := layout.Detect(root, layout.Single)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	ds, err := BuildDataset(repo, "v0")
	if err != nil {
		t.Fatalf("BuildDataset: %v", err)
	}
	if len(ds.Rows) != 0 {
		t.Errorf("got %d rows, want 0 for a repo with no results", len(ds.Rows))
	}
}

func TestTriggerStatus(t *testing.T) {
	tests := []struct {
		name   string
		result results.TriggerResult
		want   string
	}{
		{"pass", results.TriggerResult{Passed: new(true)}, statusPass},
		{"fail", results.TriggerResult{Passed: new(false)}, statusFail},
		{"no verdict", results.TriggerResult{}, statusError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := triggerStatus(tt.result); got != tt.want {
				t.Errorf("triggerStatus = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEvalStatus(t *testing.T) {
	tests := []struct {
		name   string
		result results.EvalResult
		want   string
	}{
		{"pass", results.EvalResult{Passed: new(true)}, statusPass},
		{"fail", results.EvalResult{Passed: new(false)}, statusFail},
		{"runtime error", results.EvalResult{RuntimeError: "boom", Passed: new(true)}, statusError},
		{"no verdict", results.EvalResult{}, statusError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := evalStatus(tt.result); got != tt.want {
				t.Errorf("evalStatus = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProviderModelFallback(t *testing.T) {
	// Header empty: split the model key.
	p, m := providerModel("openai/gpt-5", results.Header{})
	if p != "openai" || m != "gpt-5" {
		t.Errorf("split key: got %q/%q, want openai/gpt-5", p, m)
	}
	// Header authoritative: used as-is.
	p, m = providerModel("anything/here", results.Header{Provider: "google", Model: "gemini-3"})
	if p != "google" || m != "gemini-3" {
		t.Errorf("header: got %q/%q, want google/gemini-3", p, m)
	}
}

// TestBuildDatasetDegradesOnNewerSchema pins the viewer's read-only degrade: a
// results file written by a newer evolve yields no rows rather than an error,
// keeping the report viewer usable while the write paths refuse.
func TestBuildDatasetDegradesOnNewerSchema(t *testing.T) {
	repo, resultsDir := fixtureRepo(t)
	writeFile(t, filepath.Join(resultsDir, "results.json"), `{"schema": 99, "models": {"m": {}}}`)
	ds, err := BuildDataset(repo, "test")
	if err != nil {
		t.Fatalf("BuildDataset: %v", err)
	}
	if len(ds.Rows) != 0 {
		t.Errorf("got %d rows, want 0 (newer-schema file degrades to empty)", len(ds.Rows))
	}
}
