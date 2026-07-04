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
			Results: []results.TriggerResult{
				{Query: "Write tests | with pipes", ShouldTrigger: true, Hits: new(3), Runs: new(3), Passed: new(true), AvgRunSeconds: new(8.0)},
				{Query: "Write pytest tests", ShouldTrigger: false, Hits: new(0), Runs: new(3), Passed: new(true), AvgRunSeconds: new(8.0)},
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
			Results: []results.EvalResult{
				{ID: "basic", Passed: new(true), Summary: &results.GradeSummary{PassRate: new(1.0)},
					Timing:   &results.Timing{ExecutorDurationSeconds: new(80.0)},
					Measured: &results.Measured{InputTokens: new(8000), OutputTokens: new(3000), CostUSD: new(0.75)}},
			},
		},
		Baseline: &results.EvalSnapshot{
			RanAt: "2026-06-11T12:00:00Z",
			Summary: results.EvalSummary{
				Passed: new(0), Failed: new(1), Total: 1, PassRate: new(0.0),
				AvgRunSeconds: new(40.0),
			},
			Results: []results.EvalResult{
				{ID: "basic", Passed: new(false), Summary: &results.GradeSummary{PassRate: new(0.0)},
					Timing: &results.Timing{ExecutorDurationSeconds: new(40.0)}, Fingerprint: "fp-basic"},
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

	// Rollup row: cursor renders n/a usage cells (capability absent) and its
	// passed-count; google is count-only (— executed cells, grouped tokens).
	cursorRollup := lineContaining(t, text, "`composer-2.5`")
	if !strings.Contains(cursorRollup, "| n/a | n/a |") || !strings.Contains(cursorRollup, "| 2/2 |") {
		t.Errorf("cursor rollup row = %q", cursorRollup)
	}
	googleRollup := lineContaining(t, text, "`gemini-3.5-flash`")
	if !strings.Contains(googleRollup, "| — | — | — |") || !strings.Contains(googleRollup, "2,580") {
		t.Errorf("google rollup row = %q", googleRollup)
	}

	// Per-case detail: one heading per trigger, with a model-per-row table. The
	// query is a heading (literal pipe, not escaped) and the expectation is shown.
	if !strings.Contains(text, "#### Write tests | with pipes (expected: yes)") {
		t.Error("trigger query heading missing or pipe-escaped")
	}
	// A per-case cursor trigger row shows the verdict + hits/runs and n/a usage cells.
	cursorCase := lineWith(t, text, "composer-2.5", "PASS")
	if !strings.Contains(cursorCase, "| 2/3 |") || !strings.Contains(cursorCase, "| n/a | n/a |") {
		t.Errorf("cursor per-case row = %q", cursorCase)
	}
	// A per-case google trigger row is count-only with grouped token counts.
	googleCase := lineWith(t, text, "gemini-3.5-flash", "1,290")
	if !strings.Contains(googleCase, "| — | — | — | — |") {
		t.Errorf("google per-case row = %q", googleCase)
	}
	// Failed assertions surface with evidence, now keyed by model.
	if !strings.Contains(text, "`claude-fable-5` failed `file x exists`: x missing") {
		t.Error("failed assertion not surfaced")
	}
}

func TestCheckThresholds(t *testing.T) {
	repo := fixtureRepo(t)
	summary, err := Generate(Options{Repo: repo, ToolVersion: "test", Models: model.AllModels(nil)})
	if err != nil {
		t.Fatal(err)
	}

	// The defaults are load-bearing (report --check gates at them with no
	// config); pin the values so a silent change fails a test.
	if DefaultTriggersMinPassRate != 0.5 || DefaultEvalsMinPassRate != 0.66 {
		t.Errorf("defaults = %v/%v, want 0.5/0.66", DefaultTriggersMinPassRate, DefaultEvalsMinPassRate)
	}

	// anthropic triggers 1/2 = 50%, cursor 2/2 = 100%.
	breaches := Check(summary, Thresholds{TriggersMinPassRate: 0.8})
	if len(breaches) != 1 || !strings.Contains(breaches[0], "anthropic/claude-fable-5") {
		t.Errorf("breaches = %v, want one for anthropic", breaches)
	}

	// At the built-in defaults, anthropic triggers sit exactly on the 50% gate
	// and only its 0/1 evals rate breaches the 66% gate.
	breaches = Check(summary, Thresholds{
		TriggersMinPassRate: DefaultTriggersMinPassRate,
		EvalsMinPassRate:    DefaultEvalsMinPassRate,
	})
	if len(breaches) != 1 || !strings.Contains(breaches[0], "evals: anthropic/claude-fable-5") {
		t.Errorf("breaches = %v, want one evals breach for anthropic", breaches)
	}

	// A threshold model with no results is a breach — both gates always run, so
	// the absence surfaces once per tier.
	breaches = Check(summary, Thresholds{EvalsMinPassRate: 0.5, Models: []string{"openai/gpt-5.5"}})
	if len(breaches) != 2 {
		t.Fatalf("breaches = %v, want missing-results breaches for both tiers", breaches)
	}
	for _, b := range breaches {
		if !strings.Contains(b, "no stored results") {
			t.Errorf("breach = %q, want missing-results breach", b)
		}
	}

	if got := Check(summary, Thresholds{TriggersMinPassRate: 0.4}); len(got) != 0 {
		t.Errorf("breaches = %v, want none at 40%%", got)
	}

	// Strict holds every Defined model to the thresholds, so a configured model
	// with no results breaches per tier where the default gate (above, at 40%)
	// passes.
	strict := Check(summary, Thresholds{
		TriggersMinPassRate: 0.4,
		Strict:              true,
		Defined:             []string{"anthropic/claude-fable-5", "openai/gpt-5.5"},
	})
	if len(strict) != 2 {
		t.Fatalf("strict breaches = %v, want missing-results breaches for openai/gpt-5.5", strict)
	}
	for _, b := range strict {
		if !strings.Contains(b, "openai/gpt-5.5") {
			t.Errorf("strict breach = %q, want it to name openai/gpt-5.5", b)
		}
	}
}

// multiSkillRepo builds a single-plugin repo with two skills (aaa-skill,
// zzz-skill) to exercise the case-major detail's per-skill grouping, ordering,
// and per-case model rows. In aaa-skill, haiku ran only the first query, so the
// second query's table must omit its row.
func multiSkillRepo(t *testing.T) *layout.Repo {
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
	for _, skill := range []string{"aaa-skill", "zzz-skill"} {
		write("skills/"+skill+"/SKILL.md", "---\nname: "+skill+"\n---\nx\n")
		write("evals/"+skill+"/triggers.json",
			`{"triggers":[{"query":"q1","should_trigger":true},{"query":"q2","should_trigger":true}]}`)
	}

	mk := func(modelID string, queries ...string) *results.TriggerEntry {
		e := &results.TriggerEntry{Header: results.Header{
			Provider: "anthropic", Model: modelID, Display: modelID,
			ToolVersion: "test", RanAt: "2026-06-11T10:00:00Z", Executed: true,
			RunsPerQuery: 3, TimeoutSeconds: 120,
		}}
		for _, q := range queries {
			e.Results = append(e.Results, results.TriggerResult{
				Query: q, ShouldTrigger: true, Hits: new(3), Runs: new(3),
				Passed: new(true), AvgRunSeconds: new(9.1),
			})
		}
		e.Summary = results.TriggerSummary{Passed: new(len(queries)), Total: len(queries)}
		return e
	}

	a := &results.File{Schema: results.Schema, Plugin: "solo", Skill: "aaa-skill"}
	a.SetTrigger("anthropic/claude-fable-5", mk("claude-fable-5", "q1", "q2"))
	a.SetTrigger("anthropic/claude-haiku-4-5", mk("claude-haiku-4-5", "q1")) // missing q2
	if _, err := a.SaveDir(filepath.Join(root, "evals", "aaa-skill"), "json"); err != nil {
		t.Fatal(err)
	}
	z := &results.File{Schema: results.Schema, Plugin: "solo", Skill: "zzz-skill"}
	z.SetTrigger("anthropic/claude-fable-5", mk("claude-fable-5", "q1", "q2"))
	if _, err := z.SaveDir(filepath.Join(root, "evals", "zzz-skill"), "json"); err != nil {
		t.Fatal(err)
	}

	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestDetailMultiSkillCaseMajor(t *testing.T) {
	repo := multiSkillRepo(t)
	if _, err := Generate(Options{Repo: repo, ToolVersion: "test", Models: model.AllModels(nil)}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(repo.Root, "EVALUATION.md"))
	text := string(data)

	// Skills group under their own heading, in directory (alphabetical) order.
	ia, iz := strings.Index(text, "## aaa-skill"), strings.Index(text, "## zzz-skill")
	if ia < 0 || iz < 0 || ia > iz {
		t.Fatalf("skill headings missing or out of order: aaa=%d zzz=%d", ia, iz)
	}
	// Each query is its own subsection.
	if !strings.Contains(text, "#### q1 (expected: yes)") || !strings.Contains(text, "#### q2 (expected: yes)") {
		t.Error("per-query headings missing")
	}
	// A model missing a case is omitted from that case's table: haiku ran only q1
	// of aaa-skill, so it appears exactly twice — once in the plugin rollup, once
	// in the q1 table — and never under a q2 table.
	if n := strings.Count(text, "`claude-haiku-4-5`"); n != 2 {
		t.Errorf("haiku appears %d times, want 2 (rollup + q1 only):\n%s", n, text)
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

// lineWith returns the first line containing all needles — used to target a
// specific per-case row (e.g. a model key plus its verdict) past the rollup row.
func lineWith(t *testing.T, text string, needles ...string) string {
	t.Helper()
	for line := range strings.SplitSeq(text, "\n") {
		all := true
		for _, n := range needles {
			if !strings.Contains(line, n) {
				all = false
				break
			}
		}
		if all {
			return line
		}
	}
	t.Fatalf("no line contains all of %v:\n%s", needles, text)
	return ""
}
