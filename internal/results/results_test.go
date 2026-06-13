// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package results

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
)

func sample() *File {
	hits, runs, passed, avg := 3, 3, true, 9.1
	price := 10.0
	out := 50.0
	npassed := 1
	f := &File{Schema: Schema, Plugin: "golang", Skill: "go-testing"}
	f.SetTrigger("anthropic/claude-fable-5", &TriggerEntry{
		Header: Header{
			Provider: "anthropic", Model: "claude-fable-5", Display: "Claude Fable 5",
			ToolVersion: "0.1.0", RanAt: "2026-06-11T14:02:09Z", Executed: true,
			RunsPerQuery: 3, TimeoutSeconds: 120,
			Pricing: &Pricing{InputPerMTok: &price, OutputPerMTok: &out},
		},
		Results: []TriggerResult{{
			Query: "Write tests", ShouldTrigger: true,
			Hits: &hits, Runs: &runs, Passed: &passed, AvgRunSeconds: &avg,
			Estimate: &Estimate{InputTokens: 1385, InputCostUSD: ptr(0.01385)},
		}},
		Summary: TriggerSummary{Passed: &npassed, Total: 1, AvgRunSeconds: &avg,
			Estimate: &Estimate{InputTokens: 1385, InputCostUSD: ptr(0.01385)}},
	})
	// A cursor-style entry: no pricing, no estimates.
	chits, cruns, cpassed := 2, 3, true
	f.SetTrigger("cursor/sonnet-4.5", &TriggerEntry{
		Header: Header{
			Provider: "cursor", Model: "sonnet-4.5", Display: "Cursor — Sonnet 4.5",
			ToolVersion: "0.1.0", RanAt: "2026-06-11T15:11:40Z", Executed: true,
			RunsPerQuery: 3, TimeoutSeconds: 120, Pricing: nil,
		},
		Results: []TriggerResult{{
			Query: "Write tests", ShouldTrigger: true,
			Hits: &chits, Runs: &cruns, Passed: &cpassed,
		}},
		Summary: TriggerSummary{Passed: &npassed, Total: 1},
	})
	return f
}

func ptr[T any](v T) *T { return &v }

// saveDir writes the sample in format and returns the emitted path.
func saveDir(t *testing.T, f *File, dir, format string) string {
	t.Helper()
	path, err := f.SaveDir(dir, format)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSaveLoadRoundTrip(t *testing.T) {
	for _, format := range []string{"json", "jsonc", "yaml"} {
		t.Run(format, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "evals", "go-testing")
			saveDir(t, sample(), dir, format)
			loaded, _ := LoadDir(dir, "golang", "go-testing")
			entry := loaded.Triggers["anthropic/claude-fable-5"]
			if entry == nil || !entry.Executed || *entry.Summary.Passed != 1 {
				t.Fatalf("loaded entry = %+v", entry)
			}
			if entry.Pricing == nil || *entry.Pricing.InputPerMTok != 10.0 {
				t.Errorf("pricing = %+v", entry.Pricing)
			}
			if loaded.Triggers["cursor/sonnet-4.5"].Pricing != nil {
				t.Error("cursor pricing must stay nil")
			}
		})
	}
}

func TestSaveDeterministic(t *testing.T) {
	d1, d2 := t.TempDir(), t.TempDir()
	p1 := saveDir(t, sample(), d1, "json")
	p2 := saveDir(t, sample(), d2, "json")
	b1, _ := os.ReadFile(p1)
	b2, _ := os.ReadFile(p2)
	if string(b1) != string(b2) {
		t.Error("two saves of equal data differ")
	}
	if !strings.HasSuffix(string(b1), "}\n") {
		t.Error("missing trailing newline")
	}
}

// TestSaveDirFormatSwitch pins that history survives a format change and the
// stale sibling disappears.
func TestSaveDirFormatSwitch(t *testing.T) {
	dir := t.TempDir()
	saveDir(t, sample(), dir, "json")
	loaded, _ := LoadDir(dir, "golang", "go-testing")
	path := saveDir(t, loaded, dir, "yaml")
	if filepath.Base(path) != "results.yaml" {
		t.Errorf("path = %s", path)
	}
	if _, err := os.Stat(filepath.Join(dir, "results.json")); !os.IsNotExist(err) {
		t.Error("stale results.json must be removed on format switch")
	}
	again, _ := LoadDir(dir, "golang", "go-testing")
	if entry := again.Triggers["anthropic/claude-fable-5"]; entry == nil || *entry.Summary.Passed != 1 {
		t.Errorf("yaml reload = %+v, want history preserved", entry)
	}
	if again.Triggers["cursor/sonnet-4.5"].Pricing != nil {
		t.Error("explicit-null pricing must survive the yaml round trip")
	}
}

func TestSerializedShape(t *testing.T) {
	path := saveDir(t, sample(), t.TempDir(), "json")
	data, _ := os.ReadFile(path)
	text := string(data)

	// Explicit null pricing for cursor; omitted estimate blocks (no
	// "input_tokens": null noise).
	if !strings.Contains(text, `"pricing": null`) {
		t.Error("cursor entry must serialize pricing as explicit null")
	}
	if strings.Contains(text, `"estimate": null`) || strings.Contains(text, `"measured": null`) {
		t.Error("absent usage blocks must be omitted, not nulled")
	}
	// Model keys are provider-qualified and sorted by encoding/json.
	if strings.Index(text, "anthropic/claude-fable-5") > strings.Index(text, "cursor/sonnet-4.5") {
		t.Error("model keys not sorted")
	}
}

func TestLoadToleratesGarbage(t *testing.T) {
	missing, _ := LoadDir(t.TempDir(), "p", "s")
	if missing.Schema != Schema || missing.Plugin != "p" {
		t.Errorf("missing-file load = %+v", missing)
	}

	bad := t.TempDir()
	os.WriteFile(filepath.Join(bad, "results.json"), []byte("{corrupt"), 0o644)
	if f, _ := LoadDir(bad, "p", "s"); len(f.Triggers) != 0 {
		t.Error("corrupt file must load fresh")
	}

	old := t.TempDir()
	os.WriteFile(filepath.Join(old, "results.json"), []byte(`{"schema": 99, "models": {"m": {}}}`), 0o644)
	if f, _ := LoadDir(old, "p", "s"); len(f.Triggers) != 0 || f.Schema != Schema {
		t.Error("old-schema file must load fresh (clean break)")
	}
}

func TestGradedAssertionFlattens(t *testing.T) {
	f := &File{Schema: Schema, Plugin: "p", Skill: "s"}
	exit := 0
	graded := []GradedAssertion{{
		Assertion: evalspec.Assertion{Type: "command", Run: "go test ./...", Requires: "go", ExpectExit: &exit,
			Text: "authored text must not double-emit"},
		Text:   "command `go test ./...` exits 0",
		Passed: nil, Evidence: "skipped: go not installed",
		Source: "assertion",
	}}
	f.SetEval("anthropic/claude-fable-5", &EvalEntry{
		Header: Header{Provider: "anthropic", Model: "claude-fable-5", TimeoutSeconds: 600},
		Results: []EvalResult{{
			ID: "c1", Passed: ptr(true),
			Expectations: graded,
			Summary:      SummarizeExpectations(graded),
		}},
		Summary: EvalSummary{Total: 1},
	})
	path := saveDir(t, f, t.TempDir(), "json")
	data, _ := os.ReadFile(path)
	text := string(data)
	for _, want := range []string{
		`"expectations": [`, `"type": "command"`, `"run": "go test ./..."`,
		"\"text\": \"command `go test ./...` exits 0\"", `"passed": null`,
		`"evidence": "skipped: go not installed"`, `"source": "assertion"`,
		`"skipped": 1`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("serialized eval missing %s:\n%s", want, text)
		}
	}
	// The outer derived text shadows the embedded authored text: one key.
	if strings.Count(text, `"text":`) != 1 {
		t.Errorf("want exactly one text key:\n%s", text)
	}
	if strings.Contains(text, `"cases"`) {
		t.Error("schema 2 must name the section evals, not cases")
	}
}

func TestSummarizeExpectations(t *testing.T) {
	s := SummarizeExpectations([]GradedAssertion{
		{Passed: ptr(true)}, {Passed: ptr(true)}, {Passed: ptr(false)}, {Passed: nil},
	})
	if s.Passed != 2 || s.Failed != 1 || s.Skipped != 1 || s.Total != 4 {
		t.Errorf("summary = %+v", s)
	}
	if s.PassRate == nil || *s.PassRate != 0.666667 {
		t.Errorf("pass_rate = %v, want 0.666667 (skips excluded)", s.PassRate)
	}
	if all := SummarizeExpectations([]GradedAssertion{{Passed: nil}}); all.PassRate != nil {
		t.Errorf("all-skipped pass_rate = %v, want nil", all.PassRate)
	}
}

func TestEstimateHelpers(t *testing.T) {
	price := 10.0
	tokens := 1385
	e := NewEstimate(&tokens, &price)
	if e.InputTokens != 1385 || e.InputCostUSD == nil || *e.InputCostUSD != 0.01385 {
		t.Errorf("estimate = %+v", e)
	}
	if NewEstimate(nil, &price) != nil {
		t.Error("nil tokens must give nil estimate")
	}
	if e := NewEstimate(&tokens, nil); e.InputCostUSD != nil {
		t.Error("unpriced model must omit cost")
	}
	sum := SumEstimates([]*Estimate{e, nil, NewEstimate(&tokens, &price)})
	if sum.InputTokens != 2770 || *sum.InputCostUSD != 0.0277 {
		t.Errorf("sum = %+v", sum)
	}
	if SumEstimates([]*Estimate{nil, nil}) != nil {
		t.Error("all-nil must sum to nil")
	}
}
