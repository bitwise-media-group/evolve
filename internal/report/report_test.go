// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package report

import (
	"errors"
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

// TestPluginMaturityEmptyDir pins the item-3 cwd-safety invariant: a plugin
// whose detected directory is unavailable (empty sets, or a set with an empty
// Plugin.Dir) classifies as MaturityUnknown — pluginMaturity must never fall back
// to reading a cwd-relative .claude-plugin/plugin.json, and MaturityUnknown is
// never gated. Running from a directory that DOES hold a real 1.0.0 manifest
// proves the guard short-circuits before any such read.
func TestPluginMaturityEmptyDir(t *testing.T) {
	// A real manifest in the process cwd would classify as MaturityStable if
	// pluginMaturity ever read cwd-relative; the guard must ignore it.
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"cwd","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd) // auto-restores; fails fast if this test is ever made parallel

	if got := pluginMaturity(nil); got != MaturityUnknown {
		t.Errorf("pluginMaturity(nil) = %q, want MaturityUnknown", got)
	}
	if got := pluginMaturity([]layout.EvalSet{{Plugin: layout.Plugin{Name: "x", Dir: ""}}}); got != MaturityUnknown {
		t.Errorf("pluginMaturity(empty Dir) = %q, want MaturityUnknown (never a cwd-relative read)", got)
	}
}

// TestGated pins the gate-membership rule directly: a level in the set is gated,
// a level outside it is not, and MaturityUnknown is never gated regardless of the
// configured set — a missing or invalid manifest version can only ever WARN.
func TestGated(t *testing.T) {
	th := Thresholds{Maturity: []Maturity{MaturityStable, MaturityPrerelease}}
	if !th.gated(MaturityStable) {
		t.Error("stable is in the set, want gated")
	}
	if !th.gated(MaturityPrerelease) {
		t.Error("prerelease is in the set, want gated")
	}
	if th.gated(MaturityUnstable) {
		t.Error("unstable is not in the set, want not gated")
	}
	if th.gated(MaturityUnknown) {
		t.Error("MaturityUnknown must never be gated when it is absent from the set")
	}
	// Even if "unknown" is (improperly) placed in the set, the early guard wins.
	th2 := Thresholds{Maturity: []Maturity{MaturityUnknown}}
	if th2.gated(MaturityUnknown) {
		t.Error("MaturityUnknown must never be gated even if present in the set")
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

	// fixtureRepo's plugin is version 0.1.0 (MaturityUnstable); gate on that
	// level so these evidence issues surface as FAIL exactly as the pre-maturity
	// aggregate did, keeping this test's assertions about breach content unchanged.
	gate := []Maturity{MaturityUnstable}

	// anthropic triggers 1/2 = 50%, cursor 2/2 = 100%.
	fails, warns := Check(repo, summary, Thresholds{TriggersMinPassRate: 0.8, Maturity: gate}, nil)
	if len(fails) != 1 || !strings.Contains(fails[0], "anthropic/claude-fable-5") {
		t.Errorf("fails = %v, want one for anthropic", fails)
	}
	if len(warns) != 0 {
		t.Errorf("warns = %v, want none (plugin is gated)", warns)
	}

	// At the built-in defaults, anthropic triggers sit exactly on the 50% gate
	// and only its 0/1 evals rate breaches the 66% gate.
	fails, _ = Check(repo, summary, Thresholds{
		TriggersMinPassRate: DefaultTriggersMinPassRate,
		EvalsMinPassRate:    DefaultEvalsMinPassRate,
		Maturity:            gate,
	}, nil)
	if len(fails) != 1 || !strings.Contains(fails[0], "evals: anthropic/claude-fable-5") {
		t.Errorf("fails = %v, want one evals breach for anthropic", fails)
	}

	// A threshold model with no results is a breach — both gates always run, so
	// the absence surfaces once per tier.
	fails, _ = Check(repo, summary, Thresholds{EvalsMinPassRate: 0.5, Models: []string{"openai/gpt-5.5"}, Maturity: gate}, nil)
	if len(fails) != 2 {
		t.Fatalf("fails = %v, want missing-results breaches for both tiers", fails)
	}
	for _, b := range fails {
		if !strings.Contains(b, "no stored results") {
			t.Errorf("breach = %q, want missing-results breach", b)
		}
	}

	if got, _ := Check(repo, summary, Thresholds{TriggersMinPassRate: 0.4, Maturity: gate}, nil); len(got) != 0 {
		t.Errorf("fails = %v, want none at 40%%", got)
	}

	// Strict holds every Defined model to the thresholds, so a configured model
	// with no results breaches per tier where the default gate (above, at 40%)
	// passes.
	strict, _ := Check(repo, summary, Thresholds{
		TriggersMinPassRate: 0.4,
		Strict:              true,
		Defined:             []string{"anthropic/claude-fable-5", "openai/gpt-5.5"},
		Maturity:            gate,
	}, nil)
	if len(strict) != 2 {
		t.Fatalf("strict breaches = %v, want missing-results breaches for openai/gpt-5.5", strict)
	}
	for _, b := range strict {
		if !strings.Contains(b, "openai/gpt-5.5") {
			t.Errorf("strict breach = %q, want it to name openai/gpt-5.5", b)
		}
	}

	// An empty gated set (built-in default's zero value here, since the test
	// constructs Thresholds directly) never fails, but still surfaces the same
	// issues as warnings — evidence is not silently dropped for a non-gated plugin.
	_, warnsOnly := Check(repo, summary, Thresholds{TriggersMinPassRate: 0.8}, nil)
	if len(warnsOnly) != 1 || !strings.Contains(warnsOnly[0], "anthropic/claude-fable-5") {
		t.Errorf("warns = %v, want the same issue demoted to warn when ungated", warnsOnly)
	}
}

// staleEvalRepo builds a single-plugin (0.1.0) repo whose one eval's stored
// SpecHash cannot match its authored definition, so run.StaleTiers reports the
// evals tier stale. Pass rates are fine, isolating the staleness gate.
func staleEvalRepo(t *testing.T) *layout.Repo {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude-plugin/plugin.json", `{"name":"solo","version":"0.1.0"}`)
	write("skills/solo-skill/SKILL.md", "---\nname: solo-skill\n---\nbody\n")
	write("evals/solo-skill/triggers.json", `{"triggers":[{"query":"q","should_trigger":true}]}`)
	write("evals/solo-skill/evals.json", `{"evals":[{"id":"basic","prompt":"p","assertions":[{"type":"file_exists","path":"x"}]}]}`)

	f := &results.File{Schema: results.Schema, Plugin: "solo", Skill: "solo-skill"}
	f.SetEval("anthropic/claude-fable-5", &results.EvalEntry{
		Header: results.Header{
			Provider: "anthropic", Model: "claude-fable-5", Display: "Claude Fable 5",
			ToolVersion: "test", RanAt: "2026-06-11T10:00:00Z", Executed: true,
		},
		Summary: results.EvalSummary{Passed: new(1), Failed: new(0), Total: 1, PassRate: new(1.0)},
		// A non-empty SpecHash that cannot match the freshly hashed authored eval.
		Results: []results.EvalResult{{ID: "basic", Passed: new(true), SpecHash: "stale-does-not-match"}},
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

// TestCheckStaleEvidenceStrictOnly pins the gate's --strict scoping: stale
// evidence is inspected only under --strict (a plain --check stays a pass-rate
// gate), and under --strict the stale issue fails a gated plugin but warns an
// ungated one.
func TestCheckStaleEvidenceStrictOnly(t *testing.T) {
	repo := staleEvalRepo(t)
	summary, err := Generate(Options{Repo: repo, ToolVersion: "test", Models: model.AllModels(nil)})
	if err != nil {
		t.Fatal(err)
	}
	gate := []Maturity{MaturityUnstable} // the plugin is 0.1.0

	// Plain --check does not inspect staleness: no fails, no warns.
	if fails, warns := Check(repo, summary, Thresholds{Maturity: gate}, nil); len(fails) != 0 || len(warns) != 0 {
		t.Fatalf("non-strict: fails=%v warns=%v, want none (staleness is --strict only)", fails, warns)
	}

	// --strict surfaces the stale eval evidence; the gated plugin fails on it.
	fails, _ := Check(repo, summary, Thresholds{Strict: true, Maturity: gate}, nil)
	if len(fails) != 1 || !strings.Contains(fails[0], "stale") || !strings.Contains(fails[0], "solo-skill") {
		t.Fatalf("strict gated: fails=%v, want one stale-evidence breach for solo-skill", fails)
	}

	// The same strict issue is demoted to a warning for an ungated maturity.
	if _, warns := Check(repo, summary, Thresholds{Strict: true, Maturity: []Maturity{MaturityStable}}, nil); len(warns) != 1 || !strings.Contains(warns[0], "stale") {
		t.Fatalf("strict ungated: warns=%v, want the stale issue demoted to warn", warns)
	}
}

// staleExcludedModelRepo builds a single-plugin (0.1.0) repo whose only stored
// eval evidence — a stale entry — belongs to anthropic/claude-haiku-4-5. When
// that model is outside the active `models` restriction, its stale leftover must
// not gate the plugin; when it is inside, the staleness must surface.
func staleExcludedModelRepo(t *testing.T) *layout.Repo {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude-plugin/plugin.json", `{"name":"solo","version":"0.1.0"}`)
	write("skills/solo-skill/SKILL.md", "---\nname: solo-skill\n---\nbody\n")
	write("evals/solo-skill/evals.json",
		`{"evals":[{"id":"basic","prompt":"p","assertions":[{"type":"file_exists","path":"x"}]}]}`)

	f := &results.File{Schema: results.Schema, Plugin: "solo", Skill: "solo-skill"}
	f.SetEval("anthropic/claude-haiku-4-5", &results.EvalEntry{
		Header: results.Header{
			Provider: "anthropic", Model: "claude-haiku-4-5", Display: "Claude Haiku 4.5",
			ToolVersion: "test", RanAt: "2026-06-11T10:00:00Z", Executed: true,
		},
		Summary: results.EvalSummary{Passed: new(1), Failed: new(0), Total: 1, PassRate: new(1.0)},
		// A non-empty SpecHash that cannot match the freshly hashed authored eval.
		Results: []results.EvalResult{{ID: "basic", Passed: new(true), SpecHash: "stale-does-not-match"}},
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

// TestCheckStaleEvidenceHonorsActiveModels pins that the strict staleness path
// respects the active-model restriction, like the pass-rate path: a stale
// leftover result for a model outside the `models` set must not FAIL a gated
// plugin, but a stale result for an active model still does. Without the
// active-model filter, the staleness path (which iterates every stored model)
// would FAIL on an excluded model's leftover — a false CI failure.
func TestCheckStaleEvidenceHonorsActiveModels(t *testing.T) {
	repo := staleExcludedModelRepo(t)
	summary, err := Generate(Options{Repo: repo, ToolVersion: "test", Models: model.AllModels(nil)})
	if err != nil {
		t.Fatal(err)
	}
	gate := []Maturity{MaturityUnstable} // the plugin is 0.1.0, so gated

	// haiku is EXCLUDED from the active set: its stale leftover must not gate.
	active := map[string]bool{"anthropic/claude-fable-5": true}
	if fails, _ := Check(repo, summary, Thresholds{Strict: true, Maturity: gate}, active); len(fails) != 0 {
		t.Errorf("excluded model's stale result must not FAIL a gated plugin; fails = %v", fails)
	}

	// haiku is ACTIVE: the same stale evidence now surfaces as a failing breach.
	active = map[string]bool{"anthropic/claude-haiku-4-5": true}
	fails, _ := Check(repo, summary, Thresholds{Strict: true, Maturity: gate}, active)
	if len(fails) != 1 || !strings.Contains(fails[0], "stale") || !strings.Contains(fails[0], "solo-skill") {
		t.Errorf("active model's stale result must FAIL; fails = %v", fails)
	}

	// A nil active set means no restriction, so the stale evidence still gates.
	if nilFails, _ := Check(repo, summary, Thresholds{Strict: true, Maturity: gate}, nil); len(nilFails) != 1 {
		t.Errorf("nil active (no restriction): want the stale breach, got %v", nilFails)
	}
}

// unevidencedRepo builds a single-plugin (0.1.0) repo with authored triggers and
// evals but NO results.* file, so Generate never adds the plugin to
// summary.Plugins. The gate must still see it via the authored eval sets.
func unevidencedRepo(t *testing.T) *layout.Repo {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude-plugin/plugin.json", `{"name":"solo","version":"0.1.0"}`)
	write("skills/solo-skill/SKILL.md", "---\nname: solo-skill\n---\nbody\n")
	write("evals/solo-skill/triggers.json", `{"triggers":[{"query":"q","should_trigger":true}]}`)
	write("evals/solo-skill/evals.json",
		`{"evals":[{"id":"basic","prompt":"p","assertions":[{"type":"file_exists","path":"x"}]}]}`)

	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

// TestCheckUnevidencedPluginGated is the pinning test for the regression where a
// plugin with authored evals but zero stored results was invisible to the gate:
// Generate skips it (empty rollups), so a loop over summary.Plugins never reaches
// it, and --check --strict wrongly reported "thresholds met" for wholly
// unevidenced work. The gate now drives from the authored eval sets, so the
// missing evidence surfaces: FAIL for a gated plugin, WARN for an ungated one.
func TestCheckUnevidencedPluginGated(t *testing.T) {
	repo := unevidencedRepo(t)
	summary, err := Generate(Options{Repo: repo, ToolVersion: "test", Models: model.AllModels(nil)})
	if err != nil {
		t.Fatal(err)
	}
	// Sanity: the plugin never made it into the rollup (no stored results).
	if _, ok := summary.Plugins["solo"]; ok {
		t.Fatal("precondition failed: unevidenced plugin should not be in summary.Plugins")
	}

	// Strict holds the whole defined matrix: an unevidenced gated plugin fails,
	// once per tier per defined model, and every message names the plugin.
	fails, warns := Check(repo, summary, Thresholds{
		Strict:   true,
		Defined:  []string{"anthropic/claude-fable-5"},
		Maturity: []Maturity{MaturityUnstable}, // the plugin is 0.1.0
	}, nil)
	if len(fails) == 0 {
		t.Fatal("gated unevidenced plugin: want failing breaches, got none (regression)")
	}
	if len(warns) != 0 {
		t.Errorf("gated unevidenced plugin: warns = %v, want none", warns)
	}
	for _, f := range fails {
		if !strings.Contains(f, "no stored results") || !strings.HasPrefix(f, "solo:") {
			t.Errorf("breach = %q, want a plugin-named no-stored-results breach", f)
		}
	}

	// The same absence WARNs (never fails) when the plugin's maturity is not gated.
	f2, w2 := Check(repo, summary, Thresholds{
		Strict:   true,
		Defined:  []string{"anthropic/claude-fable-5"},
		Maturity: []Maturity{MaturityStable}, // 0.1.0 is unstable, so ungated here
	}, nil)
	if len(f2) != 0 {
		t.Errorf("ungated unevidenced plugin: fails = %v, want none", f2)
	}
	if len(w2) == 0 {
		t.Error("ungated unevidenced plugin: want warnings, got none")
	}

	// Pinned thresholds.models is the non-strict path to the same breach: a model
	// with no stored results for this plugin fails when gated.
	f3, _ := Check(repo, summary, Thresholds{
		Models:   []string{"anthropic/claude-fable-5"},
		Maturity: []Maturity{MaturityUnstable},
	}, nil)
	if len(f3) == 0 {
		t.Error("pinned-models unevidenced plugin: want failing breaches, got none")
	}
}

// singleTierRepo builds a multi-plugin (0.1.0) repo where alpha authored only
// triggers and beta only evals — the first-class single-tier shape. Neither has
// a results.* file, so the gate's strict path exercises the "only gate an
// authored tier" rule.
func singleTierRepo(t *testing.T) *layout.Repo {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// alpha: triggers only.
	write("plugins/alpha/.claude-plugin/plugin.json", `{"name":"alpha","version":"0.1.0"}`)
	write("plugins/alpha/skills/alpha-skill/SKILL.md", "---\nname: alpha-skill\n---\nbody\n")
	write("plugins/alpha/evals/alpha-skill/triggers.json", `{"triggers":[{"query":"q","should_trigger":true}]}`)
	// beta: evals only.
	write("plugins/beta/.claude-plugin/plugin.json", `{"name":"beta","version":"0.1.0"}`)
	write("plugins/beta/skills/beta-skill/SKILL.md", "---\nname: beta-skill\n---\nbody\n")
	write("plugins/beta/evals/beta-skill/evals.json",
		`{"evals":[{"id":"basic","prompt":"p","assertions":[{"type":"file_exists","path":"x"}]}]}`)

	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

// TestCheckSingleTierPlugin pins that a plugin is gated only on the tier it
// authored: a triggers-only plugin under --strict must not emit a spurious
// "evals: no stored results" breach (and vice versa). The per-plugin refactor
// removed the aggregate masking that hid this, so this is the guard against a
// false CI failure for a legitimately single-tier plugin.
func TestCheckSingleTierPlugin(t *testing.T) {
	repo := singleTierRepo(t)
	summary, err := Generate(Options{Repo: repo, ToolVersion: "test", Models: model.AllModels(nil)})
	if err != nil {
		t.Fatal(err)
	}
	fails, _ := Check(repo, summary, Thresholds{
		Strict:   true,
		Defined:  []string{"anthropic/claude-fable-5"},
		Maturity: []Maturity{MaturityUnstable}, // both plugins are 0.1.0
	}, nil)
	if len(fails) == 0 {
		t.Fatal("want breaches for the authored tiers, got none")
	}
	for _, f := range fails {
		// alpha authored no evals; beta authored no triggers. Neither tier may
		// surface a no-stored-results breach for the plugin that never defined it.
		if strings.HasPrefix(f, "alpha:") && strings.Contains(f, "evals:") {
			t.Errorf("triggers-only alpha emitted an evals breach: %q", f)
		}
		if strings.HasPrefix(f, "beta:") && strings.Contains(f, "triggers:") {
			t.Errorf("evals-only beta emitted a triggers breach: %q", f)
		}
	}
	// The authored tiers still gate: alpha's triggers and beta's evals each breach.
	var alphaTriggers, betaEvals bool
	for _, f := range fails {
		alphaTriggers = alphaTriggers || (strings.HasPrefix(f, "alpha:") && strings.Contains(f, "triggers:"))
		betaEvals = betaEvals || (strings.HasPrefix(f, "beta:") && strings.Contains(f, "evals:"))
	}
	if !alphaTriggers {
		t.Errorf("want alpha's authored triggers tier to gate; fails = %v", fails)
	}
	if !betaEvals {
		t.Errorf("want beta's authored evals tier to gate; fails = %v", fails)
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

// TestGenerateRefusesNewerSummary pins the forward-only guarantee on the report
// side: an EVALUATION rollup written by a newer evolve stops Generate before any
// file is written, wrapping results.ErrSchemaTooNew.
func TestGenerateRefusesNewerSummary(t *testing.T) {
	repo := fixtureRepo(t)
	rollup := filepath.Join(repo.Root, "EVALUATION.json")
	content := []byte(`{"schema": 99, "tool_version": "future", "plugins": {}}`)
	if err := os.WriteFile(rollup, content, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Generate(Options{Repo: repo, ToolVersion: "test", Models: model.AllModels(nil)})
	if !errors.Is(err, results.ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo.Root, "EVALUATION.md")); !os.IsNotExist(statErr) {
		t.Error("EVALUATION.md must not be written when the rollup is newer")
	}
	if after, _ := os.ReadFile(rollup); string(after) != string(content) {
		t.Error("newer rollup must survive byte-identical")
	}

	// A current-schema rollup regenerates as usual.
	if err := os.WriteFile(rollup, []byte(`{"schema": 3, "tool_version": "old", "plugins": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	summary, err := Generate(Options{Repo: repo, ToolVersion: "test", Models: model.AllModels(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Schema != SummarySchema {
		t.Errorf("summary schema = %d, want %d", summary.Schema, SummarySchema)
	}
}

// TestGenerateRefusesNewerResults pins that report generation also refuses when
// a per-skill results file (not the rollup) was written by a newer evolve.
func TestGenerateRefusesNewerResults(t *testing.T) {
	repo := fixtureRepo(t)
	path := filepath.Join(repo.Root, "evals", "solo-skill", "results.json")
	if err := os.WriteFile(path, []byte(`{"schema": 99, "models": {"m": {}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(Options{Repo: repo, ToolVersion: "test", Models: model.AllModels(nil)}); !errors.Is(err, results.ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo.Root, "EVALUATION.md")); !os.IsNotExist(statErr) {
		t.Error("EVALUATION.md must not be written when a results file is newer")
	}
}
