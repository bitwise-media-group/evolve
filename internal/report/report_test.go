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
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/results"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func ptr[T any](v T) *T { return &v }

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
			Pricing: &results.Pricing{InputPerMTok: ptr(10.0), OutputPerMTok: ptr(50.0)},
		},
		Results: []results.TriggerResult{
			{Query: "Write tests | with pipes", ShouldTrigger: true, Hits: ptr(3), Runs: ptr(3),
				Passed: ptr(true), AvgRunSeconds: ptr(9.1),
				Estimate: &results.Estimate{InputTokens: 1385, InputCostUSD: ptr(0.01385)}},
			{Query: "Write pytest tests", ShouldTrigger: false, Hits: ptr(2), Runs: ptr(3),
				Passed: ptr(false), AvgRunSeconds: ptr(5.0),
				Estimate: &results.Estimate{InputTokens: 1385, InputCostUSD: ptr(0.01385)}},
		},
		Summary: results.TriggerSummary{Passed: ptr(1), Total: 2, AvgRunSeconds: ptr(7.1),
			Estimate: &results.Estimate{InputTokens: 2770, InputCostUSD: ptr(0.0277)}},
	})
	f.SetTrigger("cursor/sonnet-4.5", &results.TriggerEntry{
		Header: results.Header{
			Provider: "cursor", Model: "sonnet-4.5", Display: "Cursor — Sonnet 4.5",
			ToolVersion: "test", RanAt: "2026-06-11T11:00:00Z", Executed: true,
			RunsPerQuery: 3, TimeoutSeconds: 120, Pricing: nil,
		},
		Results: []results.TriggerResult{
			{Query: "Write tests | with pipes", ShouldTrigger: true, Hits: ptr(2), Runs: ptr(3),
				Passed: ptr(true), AvgRunSeconds: ptr(14.3)},
			{Query: "Write pytest tests", ShouldTrigger: false, Hits: ptr(0), Runs: ptr(3),
				Passed: ptr(true), AvgRunSeconds: ptr(11.0)},
		},
		Summary: results.TriggerSummary{Passed: ptr(2), Total: 2, AvgRunSeconds: ptr(12.7)},
	})
	f.SetTrigger("google/gemini-3.5-flash", &results.TriggerEntry{
		Header: results.Header{
			Provider: "google", Model: "gemini-3.5-flash", Display: "Gemini 3.5 Flash",
			ToolVersion: "test", RanAt: "2026-06-11T09:00:00Z", Executed: false,
			TimeoutSeconds: 120,
			Pricing:        &results.Pricing{InputPerMTok: ptr(1.5), OutputPerMTok: ptr(9.0)},
		},
		Results: []results.TriggerResult{
			{Query: "Write tests | with pipes", ShouldTrigger: true,
				Estimate: &results.Estimate{InputTokens: 1290, InputCostUSD: ptr(0.001935)}},
			{Query: "Write pytest tests", ShouldTrigger: false,
				Estimate: &results.Estimate{InputTokens: 1290, InputCostUSD: ptr(0.001935)}},
		},
		Summary: results.TriggerSummary{Total: 2,
			Estimate: &results.Estimate{InputTokens: 2580, InputCostUSD: ptr(0.00387)}},
	})
	f.SetEval("anthropic/claude-fable-5", &results.EvalEntry{
		Header: results.Header{
			Provider: "anthropic", Model: "claude-fable-5", Display: "Claude Fable 5",
			ToolVersion: "test", RanAt: "2026-06-11T12:00:00Z", Executed: true,
			TimeoutSeconds: 600,
			Pricing:        &results.Pricing{InputPerMTok: ptr(10.0), OutputPerMTok: ptr(50.0)},
		},
		Results: []results.EvalResult{{
			ID: "basic", Passed: ptr(false),
			Timing:   &results.Timing{ExecutorDurationSeconds: ptr(84.2)},
			Estimate: &results.Estimate{InputTokens: 1827, InputCostUSD: ptr(0.01827)},
			Measured: &results.Measured{InputTokens: ptr(233680), OutputTokens: ptr(3142), CostUSD: ptr(0.782363)},
			Expectations: []results.GradedAssertion{
				{Text: "file x exists", Passed: ptr(false), Evidence: "x missing", Source: "assertion"},
			},
			Summary: &results.GradeSummary{Passed: 0, Failed: 1, Total: 1, PassRate: ptr(0.0)},
		}},
		Summary: results.EvalSummary{Passed: ptr(0), Failed: ptr(1), Total: 1, PassRate: ptr(0.0),
			AvgRunSeconds: ptr(84.2),
			Estimate:      &results.Estimate{InputTokens: 1827, InputCostUSD: ptr(0.01827)},
			Measured:      &results.Measured{InputTokens: ptr(233680), OutputTokens: ptr(3142), CostUSD: ptr(0.782363)}},
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
	summary, err := Generate(Options{Repo: repo, ToolVersion: "test", Providers: provider.All(nil)})
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

func TestRenderingRules(t *testing.T) {
	repo := fixtureRepo(t)
	if _, err := Generate(Options{Repo: repo, ToolVersion: "test", Providers: provider.All(nil)}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(repo.Root, "EVALUATION.md"))
	text := string(data)

	// Cursor rows render n/a for usage cells (capability absent), the
	// count-only google row renders — for the executed columns.
	cursorRow := lineContaining(t, text, "`sonnet-4.5`")
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
	summary, err := Generate(Options{Repo: repo, ToolVersion: "test", Providers: provider.All(nil)})
	if err != nil {
		t.Fatal(err)
	}

	// anthropic triggers 1/2 = 50%, cursor 2/2 = 100%.
	breaches := Check(summary, Thresholds{TriggersMinPassRate: ptr(0.8)})
	if len(breaches) != 1 || !strings.Contains(breaches[0], "anthropic/claude-fable-5") {
		t.Errorf("breaches = %v, want one for anthropic", breaches)
	}

	// A threshold model with no results is a breach.
	breaches = Check(summary, Thresholds{EvalsMinPassRate: ptr(0.5), Models: []string{"openai/gpt-5.5"}})
	if len(breaches) != 1 || !strings.Contains(breaches[0], "no stored results") {
		t.Errorf("breaches = %v, want missing-results breach", breaches)
	}

	if got := Check(summary, Thresholds{TriggersMinPassRate: ptr(0.4)}); len(got) != 0 {
		t.Errorf("breaches = %v, want none at 40%%", got)
	}
}

func lineContaining(t *testing.T, text, needle string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no line contains %q:\n%s", needle, text)
	return ""
}
