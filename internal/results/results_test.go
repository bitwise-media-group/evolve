// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package results

import (
	"errors"
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
			ContentHash:  "frontmatterhash",
			RunsPerQuery: 3, TimeoutSeconds: 120,
			Pricing: &Pricing{InputPerMTok: &price, OutputPerMTok: &out},
		},
		Results: []TriggerResult{{
			Query: "Write tests", ShouldTrigger: true,
			Hits: &hits, Runs: &runs, Passed: &passed, AvgRunSeconds: &avg,
			Estimate: &Estimate{InputTokens: 1385, InputCostUSD: new(0.01385)},
			SpecHash: "triggerspechash",
		}},
		Summary: TriggerSummary{Passed: &npassed, Total: 1, AvgRunSeconds: &avg,
			Estimate: &Estimate{InputTokens: 1385, InputCostUSD: new(0.01385)}},
	})
	// A cursor-style entry: no pricing, no estimates.
	chits, cruns, cpassed := 2, 3, true
	f.SetTrigger("cursor/composer-2.5", &TriggerEntry{
		Header: Header{
			Provider: "cursor", Model: "composer-2.5", Display: "Cursor Composer 2.5",
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
			loaded, _, _ := LoadDir(dir, "golang", "go-testing")
			entry := loaded.Trigger("anthropic/claude-fable-5")
			if entry == nil || !entry.Executed || *entry.Summary.Passed != 1 {
				t.Fatalf("loaded entry = %+v", entry)
			}
			if entry.ContentHash != "frontmatterhash" || entry.Results[0].SpecHash != "triggerspechash" {
				t.Errorf("fingerprints lost: content=%q spec=%q", entry.ContentHash, entry.Results[0].SpecHash)
			}
			if entry.Pricing == nil || *entry.Pricing.InputPerMTok != 10.0 {
				t.Errorf("pricing = %+v", entry.Pricing)
			}
			if loaded.Trigger("cursor/composer-2.5").Pricing != nil {
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
	loaded, _, _ := LoadDir(dir, "golang", "go-testing")
	path := saveDir(t, loaded, dir, "yaml")
	if filepath.Base(path) != "results.yaml" {
		t.Errorf("path = %s", path)
	}
	if _, err := os.Stat(filepath.Join(dir, "results.json")); !os.IsNotExist(err) {
		t.Error("stale results.json must be removed on format switch")
	}
	again, _, _ := LoadDir(dir, "golang", "go-testing")
	if entry := again.Trigger("anthropic/claude-fable-5"); entry == nil || *entry.Summary.Passed != 1 {
		t.Errorf("yaml reload = %+v, want history preserved", entry)
	}
	if again.Trigger("cursor/composer-2.5").Pricing != nil {
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
	if strings.Index(text, "anthropic/claude-fable-5") > strings.Index(text, "cursor/composer-2.5") {
		t.Error("model keys not sorted")
	}
}

func TestLoadToleratesGarbage(t *testing.T) {
	missing, _, _ := LoadDir(t.TempDir(), "p", "s")
	if missing.Schema != Schema || missing.Plugin != "p" {
		t.Errorf("missing-file load = %+v", missing)
	}

	bad := t.TempDir()
	os.WriteFile(filepath.Join(bad, "results.json"), []byte("{corrupt"), 0o644)
	if f, _, _ := LoadDir(bad, "p", "s"); len(f.Models) != 0 {
		t.Error("corrupt file must load fresh")
	}

}

// TestLoadRefusesNewerSchema pins the forward-only guarantee: a file written by
// a newer evolve is never discarded or read down — LoadDir surfaces
// ErrSchemaTooNew (reset=false) alongside a fresh value and leaves the file
// untouched, so callers on the write path refuse before overwriting committed
// data.
func TestLoadRefusesNewerSchema(t *testing.T) {
	newer := t.TempDir()
	path := filepath.Join(newer, "results.json")
	content := []byte(`{"schema": 99, "models": {"m": {}}}`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	f, reset, err := LoadDir(newer, "p", "s")
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}
	if reset || len(f.Models) != 0 || f.Schema != Schema {
		t.Errorf("f = %+v reset = %v, want fresh file without reset", f, reset)
	}
	if after, _ := os.ReadFile(path); string(after) != string(content) {
		t.Error("newer-schema file must be left untouched")
	}
}

// TestLoadMigratesOlderSchema pins the auto-upgrade: a file written under the old
// (v4) structural shape — tier-major maps with snapshot case maps — is migrated in
// place to the current model-major shape (snapshot results arrays), not discarded.
func TestLoadMigratesOlderSchema(t *testing.T) {
	dir := t.TempDir()
	const v4 = `{
  "schema": 4, "plugin": "p", "skill": "s",
  "triggers": {
    "fake/m1": {
      "provider": "fake", "model": "m1", "executed": true, "ran_at": "2026-06-10T00:00:00Z",
      "results": [{"query": "q1", "should_trigger": true, "hits": 2, "runs": 3, "passed": true}],
      "summary": {"passed": 1, "total": 1},
      "previous": {
        "ran_at": "2026-06-09T00:00:00Z", "summary": {"passed": 0, "total": 1},
        "cases": {"q1": {"hits": 0, "runs": 3, "passed": false, "avg_run_seconds": 8.0}}
      }
    }
  },
  "evals": {
    "fake/m1": {
      "provider": "fake", "model": "m1", "executed": true, "ran_at": "2026-06-10T00:00:00Z",
      "results": [{"id": "e1", "passed": false, "summary": {"passed": 0, "failed": 1, "total": 1}}],
      "summary": {"passed": 0, "failed": 1, "total": 1},
      "baseline": {
        "ran_at": "2026-06-10T00:00:00Z", "summary": {"passed": 1, "total": 1},
        "cases": {"e1": {"passed": true, "pass_rate": 1.0, "assert_passed": 1, "assert_total": 1, "avg_run_seconds": 4.0, "fingerprint": "fp-e1"}}
      }
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "results.json"), []byte(v4), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, reset, _ := LoadDir(dir, "p", "s")
	if reset {
		t.Fatal("an older structural schema must migrate, not reset")
	}
	if loaded.Schema != Schema {
		t.Errorf("schema = %d, want upgraded to %d", loaded.Schema, Schema)
	}
	// Trigger history survives under the new model-major shape, as a results array.
	tr := loaded.Trigger("fake/m1")
	if tr == nil || tr.Previous == nil || len(tr.Previous.Results) != 1 {
		t.Fatalf("trigger previous lost in migration: %+v", tr)
	}
	if pc := tr.Previous.Results[0]; pc.Query != "q1" || pc.Hits == nil || *pc.Hits != 0 {
		t.Errorf("migrated trigger snapshot case = %+v", pc)
	}
	// Eval baseline survives, with the case metrics reprojected onto an EvalResult.
	ev := loaded.Eval("fake/m1")
	if ev == nil || ev.Baseline == nil || len(ev.Baseline.Results) != 1 {
		t.Fatalf("eval baseline lost in migration: %+v", ev)
	}
	bc := ev.Baseline.Results[0]
	if bc.ID != "e1" || bc.Fingerprint != "fp-e1" || bc.Summary == nil || bc.Summary.PassRate == nil || *bc.Summary.PassRate != 1.0 {
		t.Errorf("migrated baseline case = %+v (summary %+v)", bc, bc.Summary)
	}
	if rs := bc.RunSeconds(); rs == nil || *rs != 4.0 {
		t.Errorf("migrated baseline run seconds = %v, want 4.0", rs)
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
			ID: "c1", Passed: new(true),
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

func TestRuntimeErrorSerialization(t *testing.T) {
	f := &File{Schema: Schema, Plugin: "p", Skill: "s"}
	errored := 1
	f.SetEval("anthropic/claude-fable-5", &EvalEntry{
		Header:  Header{Provider: "anthropic", Model: "claude-fable-5", TimeoutSeconds: 600, Executed: true},
		Results: []EvalResult{{ID: "c1", RuntimeError: "empty CLI output"}}, // Passed stays nil
		Summary: EvalSummary{Total: 1, Errored: &errored},
	})
	dir := t.TempDir()
	path := saveDir(t, f, dir, "json")
	text := string(mustRead(t, path))
	if !strings.Contains(text, `"runtime_error": "empty CLI output"`) {
		t.Errorf("missing runtime_error:\n%s", text)
	}
	if !strings.Contains(text, `"errored": 1`) {
		t.Errorf("missing errored count:\n%s", text)
	}
	if strings.Contains(text, `"passed"`) {
		t.Errorf("an errored result/summary must omit passed:\n%s", text)
	}

	// Additive fields must not trigger a schema reset; values round-trip.
	loaded, reset, _ := LoadDir(dir, "p", "s")
	if reset {
		t.Error("additive omitempty fields must not force a schema reset")
	}
	entry := loaded.Eval("anthropic/claude-fable-5")
	if r := entry.Results[0]; r.RuntimeError != "empty CLI output" || r.Passed != nil {
		t.Errorf("loaded result = %+v", r)
	}
	if e := entry.Summary.Errored; e == nil || *e != 1 {
		t.Errorf("loaded errored = %v, want 1", e)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSummarizeExpectations(t *testing.T) {
	s := SummarizeExpectations([]GradedAssertion{
		{Passed: new(true)}, {Passed: new(true)}, {Passed: new(false)}, {Passed: nil},
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
