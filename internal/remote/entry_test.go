// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/results"
)

// oversize is comfortably past MaxEntryBytes on its own.
var oversize = strings.Repeat("x", MaxEntryBytes+1024)

func TestMarshalTriggerEntryFitsUntouched(t *testing.T) {
	passed := 1
	e := &results.TriggerEntry{
		Summary:  results.TriggerSummary{Passed: &passed, Total: 1},
		Previous: &results.TriggerSnapshot{Results: []results.TriggerResult{{Query: "q"}}},
	}
	raw, err := MarshalTriggerEntry(e)
	if err != nil {
		t.Fatalf("MarshalTriggerEntry: %v", err)
	}
	var got results.TriggerEntry
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Previous == nil || len(got.Previous.Results) != 1 {
		t.Errorf("a fitting entry must keep its snapshot results, got %+v", got.Previous)
	}
}

func TestMarshalTriggerEntryDropsSnapshotResults(t *testing.T) {
	passed := 1
	e := &results.TriggerEntry{
		Results: []results.TriggerResult{{Query: "live"}},
		Summary: results.TriggerSummary{Passed: &passed, Total: 1},
		Previous: &results.TriggerSnapshot{
			Summary: results.TriggerSummary{Total: 1},
			Results: []results.TriggerResult{{Query: oversize}},
		},
	}
	raw, err := MarshalTriggerEntry(e)
	if err != nil {
		t.Fatalf("MarshalTriggerEntry: %v", err)
	}
	if len(raw) > MaxEntryBytes {
		t.Fatalf("entry is %d bytes, cap %d", len(raw), MaxEntryBytes)
	}
	var got results.TriggerEntry
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Previous == nil {
		t.Fatal("the snapshot summary must survive the trim")
	}
	if got.Previous.Results != nil {
		t.Error("snapshot results survived past the cap")
	}
	if len(got.Results) != 1 || got.Results[0].Query != "live" {
		t.Errorf("live results must survive untouched, got %+v", got.Results)
	}
}

func TestMarshalTriggerEntryHopeless(t *testing.T) {
	e := &results.TriggerEntry{Results: []results.TriggerResult{{Query: oversize}}}
	if _, err := MarshalTriggerEntry(e); err == nil {
		t.Fatal("an entry whose live results exceed the cap must error, not ship oversize")
	}
}

func TestMarshalEvalEntrySheddingStopsWhenItFits(t *testing.T) {
	pass := true
	e := &results.EvalEntry{
		Results: []results.EvalResult{{
			ID:     "e1",
			Passed: &pass,
			Expectations: []results.GradedAssertion{
				{Text: "keep me", Passed: &pass, Evidence: "small evidence"},
			},
		}},
		Baseline: &results.EvalSnapshot{Results: []results.EvalResult{{ID: oversize}}},
		Previous: &results.EvalSnapshot{Results: []results.EvalResult{{ID: oversize}}},
	}
	raw, err := MarshalEvalEntry(e)
	if err != nil {
		t.Fatalf("MarshalEvalEntry: %v", err)
	}
	var got results.EvalEntry
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Baseline == nil || got.Previous == nil {
		t.Fatal("snapshot summaries must survive")
	}
	if got.Baseline.Results != nil || got.Previous.Results != nil {
		t.Error("snapshot results survived past the cap")
	}
	// Shedding stops at the first sufficient stage: live evidence untouched.
	if ev := got.Results[0].Expectations[0].Evidence; ev != "small evidence" {
		t.Errorf("live evidence trimmed prematurely: %q", ev)
	}
}

func TestMarshalEvalEntryTruncatesEvidence(t *testing.T) {
	big := strings.Repeat("e", MaxEntryBytes/2)
	e := &results.EvalEntry{
		Results: []results.EvalResult{{
			ID: "e1",
			Expectations: []results.GradedAssertion{
				{Text: "a", Evidence: big},
				{Text: "b", Evidence: big},
				{Text: "c", Evidence: big},
			},
		}},
	}
	raw, err := MarshalEvalEntry(e)
	if err != nil {
		t.Fatalf("MarshalEvalEntry: %v", err)
	}
	if len(raw) > MaxEntryBytes {
		t.Fatalf("entry is %d bytes, cap %d", len(raw), MaxEntryBytes)
	}
	var got results.EvalEntry
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	exps := got.Results[0].Expectations
	if len(exps) != 3 {
		t.Fatalf("expectations dropped when truncation sufficed: %d left", len(exps))
	}
	for _, exp := range exps {
		if len(exp.Evidence) > evidenceCap {
			t.Errorf("evidence %q is %d bytes, cap %d", exp.Text, len(exp.Evidence), evidenceCap)
		}
		if !strings.HasSuffix(exp.Evidence, "…") {
			t.Errorf("truncated evidence lacks the ellipsis marker: %q…", exp.Evidence[:16])
		}
	}
}

func TestMarshalEvalEntryDropsExpectationsLast(t *testing.T) {
	// Many expectations whose weight is in Text (which truncation never
	// touches): only the final shrink — dropping expectations — can fit it.
	text := strings.Repeat("t", 1024)
	exps := make([]results.GradedAssertion, 1024)
	for i := range exps {
		exps[i] = results.GradedAssertion{Text: text, Evidence: "tiny"}
	}
	passed := 1
	e := &results.EvalEntry{
		Results: []results.EvalResult{{ID: "e1", Expectations: exps}},
		Summary: results.EvalSummary{Passed: &passed, Total: 1},
	}
	raw, err := MarshalEvalEntry(e)
	if err != nil {
		t.Fatalf("MarshalEvalEntry: %v", err)
	}
	var got results.EvalEntry
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Results[0].Expectations != nil {
		t.Error("expectations survived past the cap")
	}
	if got.Summary.Total != 1 || got.Results[0].ID != "e1" {
		t.Error("the result identity and summary must survive the trim")
	}
}

func TestMarshalEvalEntryHopeless(t *testing.T) {
	// Weight in a field no shrink reaches.
	e := &results.EvalEntry{Results: []results.EvalResult{{ID: "e1", RuntimeError: oversize}}}
	if _, err := MarshalEvalEntry(e); err == nil {
		t.Fatal("an untrimmable oversize entry must error, not ship oversize")
	}
}
