// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package web

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/results"
)

// fixtureRepo writes a single-plugin repository with one skill whose results
// file carries a model with both trigger and eval entries, and returns the
// detected repo plus the skill's results directory. EvalSets only enumerates a
// skill that defines triggers/evals, so stub definition files are written too.
func fixtureRepo(t *testing.T) (repo *layout.Repo, resultsDir string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"demo"}`)

	resultsDir = filepath.Join(root, "evals", "greeter")
	writeFile(t, filepath.Join(resultsDir, "triggers.json"), `[]`)
	writeFile(t, filepath.Join(resultsDir, "evals.json"), `[]`)
	if _, err := sampleFile().SaveDir(resultsDir, "json"); err != nil {
		t.Fatalf("save results: %v", err)
	}

	repo, err := layout.Detect(root, layout.Single)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	return repo, resultsDir
}

// sampleFile is the canonical results file the fixtures load: one model with two
// trigger queries (one pass, one fail) and two evals (one pass, one error).
func sampleFile() *results.File {
	header := results.Header{
		Provider: "anthropic", Model: "claude-sonnet-4-6", Display: "Claude Sonnet 4.6",
		Harness: "claude", Executed: true, RanAt: "2026-06-01T00:00:00Z",
	}
	return &results.File{
		Schema: results.Schema, Plugin: "demo", Skill: "greeter",
		Models: map[string]*results.ModelEntry{
			"anthropic/claude-sonnet-4-6": {
				Triggers: &results.TriggerEntry{
					Header: header,
					Results: []results.TriggerResult{
						{Query: "please greet me", ShouldTrigger: true, Hits: new(3), Runs: new(3),
							Passed: new(true), AvgRunSeconds: new(1.2),
							Estimate: &results.Estimate{InputTokens: 1200, InputCostUSD: new(0.004)}},
						{Query: "what is the weather", ShouldTrigger: false, Hits: new(2), Runs: new(3),
							Passed: new(false)},
					},
				},
				Evals: &results.EvalEntry{
					Header: header,
					Results: []results.EvalResult{
						{ID: "greets-politely", Name: "greets politely", Passed: new(true),
							Measured: &results.Measured{InputTokens: new(1000), OutputTokens: new(200), CostUSD: new(0.01)},
							Timing:   &results.Timing{ExecutorDurationSeconds: new(4.5)}},
						{ID: "handles-empty", Name: "handles empty input", RuntimeError: "auth blocked"},
					},
				},
			},
		},
	}
}

// writeFile creates parent directories and writes content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// waitFor polls cond until it is true or the deadline passes.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", d, what)
}

// rowByID finds the first row with the given id, or fails.
func rowByID(t *testing.T, rows []Row, id string) Row {
	t.Helper()
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no row with id %q", id)
	return Row{}
}
