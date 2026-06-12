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

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evals", "go-testing", "results.json")
	f := sample()
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded := Load(path, "golang", "go-testing")
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
}

func TestSaveDeterministic(t *testing.T) {
	dir := t.TempDir()
	p1, p2 := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	if err := sample().Save(p1); err != nil {
		t.Fatal(err)
	}
	if err := sample().Save(p2); err != nil {
		t.Fatal(err)
	}
	b1, _ := os.ReadFile(p1)
	b2, _ := os.ReadFile(p2)
	if string(b1) != string(b2) {
		t.Error("two saves of equal data differ")
	}
	if !strings.HasSuffix(string(b1), "}\n") {
		t.Error("missing trailing newline")
	}
}

func TestSerializedShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	if err := sample().Save(path); err != nil {
		t.Fatal(err)
	}
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
	dir := t.TempDir()
	missing := Load(filepath.Join(dir, "nope.json"), "p", "s")
	if missing.Schema != Schema || missing.Plugin != "p" {
		t.Errorf("missing-file load = %+v", missing)
	}

	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{corrupt"), 0o644)
	if f := Load(bad, "p", "s"); len(f.Triggers) != 0 {
		t.Error("corrupt file must load fresh")
	}

	old := filepath.Join(dir, "old.json")
	os.WriteFile(old, []byte(`{"schema": 2, "models": {"m": {}}}`), 0o644)
	if f := Load(old, "p", "s"); len(f.Triggers) != 0 || f.Schema != Schema {
		t.Error("old-schema file must load fresh (clean break)")
	}
}

func TestGradedAssertionFlattens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	f := &File{Schema: Schema, Plugin: "p", Skill: "s"}
	exit := 0
	f.SetCase("anthropic/claude-fable-5", &CaseEntry{
		Header: Header{Provider: "anthropic", Model: "claude-fable-5", TimeoutSeconds: 600},
		Results: []CaseResult{{
			ID: "c1", Passed: ptr(true),
			Assertions: []GradedAssertion{{
				Assertion: evalspec.Assertion{Type: "command", Run: "go test ./...", Requires: "go", ExpectExit: &exit},
				Passed:    nil, Evidence: "skipped: go not installed",
			}},
		}},
		Summary: CaseSummary{Total: 1},
	})
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(path)
	for _, want := range []string{`"type": "command"`, `"run": "go test ./..."`, `"passed": null`, `"evidence": "skipped: go not installed"`} {
		if !strings.Contains(string(text), want) {
			t.Errorf("serialized case missing %s:\n%s", want, text)
		}
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
