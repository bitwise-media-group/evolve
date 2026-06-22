// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package report

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/results"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// fixtureRepo builds a temp single-plugin repo with one skill's results
// covering the three provider shapes: full anthropic data, a cursor entry
// (no usage, null pricing), and a count-only google entry.
func fixtureRepo(t *testing.T) *layout.Repo {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude-plugin/plugin.json", `{"name":"solo","version":"0.1.0"}`)
	write("skills/solo-skill/SKILL.md", "---\nname: solo-skill\n---\nx\n")
	write("evals/solo-skill/triggers.json", `{"triggers":[{"query":"q","should_trigger":true}]}`)
	write("evals/solo-skill/evals.json", `{"evals":[{"id":"basic","prompt":"p","assertions":[{"type":"file_exists","path":"x"}]}]}`)

	f := &results.File{Schema: results.Schema, Plugin: "solo", Skill: "solo-skill"}
	f.SetTrigger("anthropic/claude-fable-5", &results.TriggerEntry{
		Header: results.Header{
			Provider: "anthropic", Model: "claude-fable-5", Display: "Claude Fable 5",
			ToolVersion: "test", RanAt: "2026-06-11T10:00:00Z", Executed: true,
			RunsPerQuery: 3, TimeoutSeconds: 120,
			Pricing: &results.Pricing{InputPerMTok: new(10.0), OutputPerMTok: new(50.0)},
		},
		Results: []results.TriggerResult{
			{
				Query: "Write tests | with pipes", ShouldTrigger: true, Hits: new(3), Runs: new(3),
				Passed: new(true), AvgRunSeconds: new(9.1),
				Estimate: &results.Estimate{InputTokens: 1385, InputCostUSD: new(0.01385)},
			},
			{
				Query: "Write pytest tests", ShouldTrigger: false, Hits: new(2), Runs: new(3),
				Passed: new(false), AvgRunSeconds: new(5.0),
				Estimate: &results.Estimate{InputTokens: 1385, InputCostUSD: new(0.01385)},
			},
		},
		Summary: results.TriggerSummary{
			Passed: new(1), Total: 2, AvgRunSeconds: new(7.1),
			Estimate: &results.Estimate{InputTokens: 2770, InputCostUSD: new(0.0277)},
		},
		Previous: &results.TriggerSnapshot{
			RanAt: "2026-06-10T10:00:00Z",
			Summary: results.TriggerSummary{
				Passed: new(2), Total: 2, AvgRunSeconds: new(8.0),
				Estimate: &results.Estimate{InputTokens: 2700, InputCostUSD: new(0.027)},
			},
			Cases: map[string]results.TriggerCaseMetrics{
				"Write tests | with pipes": {Hits: new(3), Runs: new(3), Passed: new(true), AvgRunSeconds: new(8.0)},
				"Write pytest tests":       {Hits: new(0), Runs: new(3), Passed: new(true), AvgRunSeconds: new(8.0)},
			},
		},
	})
	f.SetTrigger("cursor/composer-2.5", &results.TriggerEntry{
		Header: results.Header{
			Provider: "cursor", Model: "composer-2.5", Display: "Cursor Composer 2.5",
			ToolVersion: "test", RanAt: "2026-06-11T11:00:00Z", Executed: true,
			RunsPerQuery: 3, TimeoutSeconds: 120, Pricing: nil,
		},
		Results: []results.TriggerResult{
			{
				Query: "Write tests | with pipes", ShouldTrigger: true, Hits: new(2), Runs: new(3),
				Passed: new(true), AvgRunSeconds: new(14.3),
			},
			{
				Query: "Write pytest tests", ShouldTrigger: false, Hits: new(0), Runs: new(3),
				Passed: new(true), AvgRunSeconds: new(11.0),
			},
		},
		Summary: results.TriggerSummary{Passed: new(2), Total: 2, AvgRunSeconds: new(12.7)},
	})
	f.SetTrigger("google/gemini-3.5-flash", &results.TriggerEntry{
		Header: results.Header{
			Provider: "google", Model: "gemini-3.5-flash", Display: "Gemini 3.5 Flash",
			ToolVersion: "test", RanAt: "2026-06-11T09:00:00Z", Executed: false,
			TimeoutSeconds: 120,
			Pricing:        &results.Pricing{InputPerMTok: new(1.5), OutputPerMTok: new(9.0)},
		},
		Results: []results.TriggerResult{
			{
				Query: "Write tests | with pipes", ShouldTrigger: true,
				Estimate: &results.Estimate{InputTokens: 1290, InputCostUSD: new(0.001935)},
			},
			{
				Query: "Write pytest tests", ShouldTrigger: false,
				Estimate: &results.Estimate{InputTokens: 1290, InputCostUSD: new(0.001935)},
			},
		},
		Summary: results.TriggerSummary{
			Total:    2,
			Estimate: &results.Estimate{InputTokens: 2580, InputCostUSD: new(0.00387)},
		},
	})
	f.SetEval("anthropic/claude-fable-5", &results.EvalEntry{
		Header: results.Header{
			Provider: "anthropic", Model: "claude-fable-5", Display: "Claude Fable 5",
			ToolVersion: "test", RanAt: "2026-06-11T12:00:00Z", Executed: true,
			TimeoutSeconds: 600,
			Pricing:        &results.Pricing{InputPerMTok: new(10.0), OutputPerMTok: new(50.0)},
		},
		Results: []results.EvalResult{{
			ID: "basic", Passed: new(false),
			Timing:   &results.Timing{ExecutorDurationSeconds: new(84.2)},
			Estimate: &results.Estimate{InputTokens: 1827, InputCostUSD: new(0.01827)},
			Measured: &results.Measured{InputTokens: new(8200), CacheReadTokens: new(220000), CacheCreationTokens: new(5480), OutputTokens: new(3142), CostUSD: new(0.782363)},
			Expectations: []results.GradedAssertion{
				{Text: "file x exists", Passed: new(false), Evidence: "x missing", Source: "assertion"},
			},
			Summary: &results.GradeSummary{Passed: 0, Failed: 1, Total: 1, PassRate: new(0.0)},
		}},
		Summary: results.EvalSummary{
			Passed: new(0), Failed: new(1), Total: 1, PassRate: new(0.0),
			AvgRunSeconds: new(84.2),
			Estimate:      &results.Estimate{InputTokens: 1827, InputCostUSD: new(0.01827)},
			Measured:      &results.Measured{InputTokens: new(8200), CacheReadTokens: new(220000), CacheCreationTokens: new(5480), OutputTokens: new(3142), CostUSD: new(0.782363)},
		},
		Previous: &results.EvalSnapshot{
			RanAt: "2026-06-10T12:00:00Z",
			Summary: results.EvalSummary{
				Passed: new(1), Failed: new(0), Total: 1, PassRate: new(1.0),
				AvgRunSeconds: new(80.0),
				Measured:      &results.Measured{InputTokens: new(8000), OutputTokens: new(3000), CostUSD: new(0.75)},
			},
			Cases: map[string]results.EvalCaseMetrics{
				"basic": {
					Passed: new(true), PassRate: new(1.0), AvgRunSeconds: new(80.0),
					Measured: &results.Measured{InputTokens: new(8000), OutputTokens: new(3000), CostUSD: new(0.75)},
				},
			},
		},
		Baseline: &results.EvalSnapshot{
			RanAt: "2026-06-11T12:00:00Z",
			Summary: results.EvalSummary{
				Passed: new(0), Failed: new(1), Total: 1, PassRate: new(0.0),
				AvgRunSeconds: new(40.0),
			},
			Cases: map[string]results.EvalCaseMetrics{
				"basic": {Passed: new(false), PassRate: new(0.0), AvgRunSeconds: new(40.0), Fingerprint: "fp-basic"},
			},
		},
	})
	if _, err := f.SaveDir(filepath.Join(root, "evals", "solo-skill"), "json"); err != nil {
		t.Fatal(err)
	}

	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestGenerateGolden(t *testing.T) {
	repo := fixtureRepo(t)
	summary, err := Generate(Options{Repo: repo, ToolVersion: "test", Models: model.AllModels(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if summary.LatestRun != "2026-06-11T12:00:00Z" {
		t.Errorf("latest_run = %s", summary.LatestRun)
	}

	for golden, generated := range map[string]string{
		"root.md":      filepath.Join(repo.Root, "EVALUATION.md"),
		"summary.json": filepath.Join(repo.Root, "EVALUATION.json"),
	} {
		got, err := os.ReadFile(generated)
		if err != nil {
			t.Fatal(err)
		}
		goldenPath := filepath.Join("..", "..", "e2e", "golden", golden)
		if *update {
			os.MkdirAll(filepath.Dir(goldenPath), 0o755)
			if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("missing golden %s (run: go test ./internal/report -update): %v", golden, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs from golden.\n--- got ---\n%s", golden, got)
		}
	}

	// Single layout: no per-plugin page.
	if _, err := os.Stat(filepath.Join(repo.Root, "plugins")); err == nil {
		t.Error("single layout must not create a plugins dir")
	}
}

// TestGenerateFiltersToActive checks that a configured `models` restriction
// filters the report to the active models and lists the rest in the excluded note.
func TestGenerateFiltersToActive(t *testing.T) {
	repo := fixtureRepo(t)
	active := map[string]bool{
		"anthropic/claude-fable-5": true,
		"cursor/composer-2.5":      true,
	}
	if _, err := Generate(Options{
		Repo: repo, ToolVersion: "test", Models: model.AllModels(nil), ActiveModels: active,
	}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(repo.Root, "EVALUATION.md"))
	text := string(data)

	if !strings.Contains(text, "## Excluded models") {
		t.Fatalf("missing excluded-models note:\n%s", text)
	}
	// Google has no active model: listed as all excluded, and its result row is
	// dropped from the tables entirely.
	if !strings.Contains(text, "| Google | all models |") {
		t.Errorf("excluded note missing Google all-models row:\n%s", text)
	}
	if strings.Contains(text, "gemini-3.5-flash") {
		t.Error("filtered google model still present in report")
	}
	// Anthropic is partially excluded: its non-active models are listed by id.
	if !strings.Contains(text, "claude-haiku-4-5") {
		t.Errorf("excluded note missing partial anthropic ids:\n%s", text)
	}
	// Active models survive in the tables.
	if !strings.Contains(text, "composer-2.5") || !strings.Contains(text, "claude-fable-5") {
		t.Error("active models missing from filtered report")
	}
}

func TestRenderingRules(t *testing.T) {
	repo := fixtureRepo(t)
	if _, err := Generate(Options{Repo: repo, ToolVersion: "test", Models: model.AllModels(nil)}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(repo.Root, "EVALUATION.md"))
	text := string(data)

	// Cursor rows render n/a for usage cells (capability absent), the
	// count-only google row renders — for the executed columns.
	cursorRow := lineContaining(t, text, "`composer-2.5`")
	for _, cell := range []string{"| n/a | n/a |"} {
		if !strings.Contains(cursorRow, cell) {
			t.Errorf("cursor row = %q, want usage cells %q", cursorRow, cell)
		}
	}
	if !strings.Contains(cursorRow, "| 2/2 |") {
		t.Errorf("cursor row = %q, want passed cell", cursorRow)
	}
	googleRow := lineContaining(t, text, "`gemini-3.5-flash`")
	if !strings.Contains(googleRow, "| — | — | — |") {
		t.Errorf("google count-only row = %q, want — executed cells", googleRow)
	}
	if !strings.Contains(googleRow, "2,580") {
		t.Errorf("google row = %q, want grouped token count", googleRow)
	}

	// Pipes in queries must be escaped in detail tables.
	if !strings.Contains(text, `Write tests \| with pipes`) {
		t.Error("query pipes not escaped")
	}
	// Failed assertions surface with evidence.
	if !strings.Contains(text, "`basic` failed") {
		t.Error("failed assertion not surfaced")
	}
}

func TestCheckThresholds(t *testing.T) {
	repo := fixtureRepo(t)
	summary, err := Generate(Options{Repo: repo, ToolVersion: "test", Models: model.AllModels(nil)})
	if err != nil {
		t.Fatal(err)
	}

	// anthropic triggers 1/2 = 50%, cursor 2/2 = 100%.
	breaches := Check(summary, Thresholds{TriggersMinPassRate: new(0.8)})
	if len(breaches) != 1 || !strings.Contains(breaches[0], "anthropic/claude-fable-5") {
		t.Errorf("breaches = %v, want one for anthropic", breaches)
	}

	// A threshold model with no results is a breach.
	breaches = Check(summary, Thresholds{EvalsMinPassRate: new(0.5), Models: []string{"openai/gpt-5.5"}})
	if len(breaches) != 1 || !strings.Contains(breaches[0], "no stored results") {
		t.Errorf("breaches = %v, want missing-results breach", breaches)
	}

	if got := Check(summary, Thresholds{TriggersMinPassRate: new(0.4)}); len(got) != 0 {
		t.Errorf("breaches = %v, want none at 40%%", got)
	}
}

func lineContaining(t *testing.T, text, needle string) string {
	t.Helper()
	for line := range strings.SplitSeq(text, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no line contains %q:\n%s", needle, text)
	return ""
}
