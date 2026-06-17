// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import (
	"strings"
	"testing"
)

func keys(sel []Selection) []string {
	out := make([]string, len(sel))
	for i, s := range sel {
		out[i] = s.Key()
	}
	return out
}

func TestSelect(t *testing.T) {
	providers := All(nil)
	tests := []struct {
		spec    string
		want    []string // expected keys, or nil to assert only count>0
		wantErr string
	}{
		{spec: "", want: []string{
			"anthropic/claude-haiku-4-5", "anthropic/claude-sonnet-4-6",
			"anthropic/claude-opus-4-8", "anthropic/claude-fable-5"}},
		{spec: "anthropic", want: []string{
			"anthropic/claude-haiku-4-5", "anthropic/claude-sonnet-4-6",
			"anthropic/claude-opus-4-8", "anthropic/claude-fable-5"}},
		{spec: "claude-fable-5", want: []string{"anthropic/claude-fable-5"}},
		{spec: "cursor/sonnet-4.5", want: []string{"cursor/sonnet-4.5"}},
		{spec: "copilot/claude-sonnet-4.6", want: []string{"copilot/claude-sonnet-4.6"}},
		{spec: "claude-fable-5, gpt-5.5", want: []string{"anthropic/claude-fable-5", "openai/gpt-5.5"}},
		{spec: "anthropic,claude-fable-5", want: []string{ // dedup
			"anthropic/claude-haiku-4-5", "anthropic/claude-sonnet-4-6",
			"anthropic/claude-opus-4-8", "anthropic/claude-fable-5"}},
		{spec: "nope", wantErr: `unknown provider or model in --models: "nope"`},
		{spec: "cursor/claude-fable-5", wantErr: "unknown provider or model"},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := Select(tt.spec, providers)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			gotKeys := keys(got)
			if strings.Join(gotKeys, " ") != strings.Join(tt.want, " ") {
				t.Errorf("keys = %v, want %v", gotKeys, tt.want)
			}
		})
	}
}

func TestSelectAll(t *testing.T) {
	got, err := Select("all", All(nil))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	seen := map[string]bool{}
	for _, s := range got {
		if !seen[s.Provider.Name()] {
			seen[s.Provider.Name()] = true
			names = append(names, s.Provider.Name())
		}
	}
	if want := "anthropic openai google cursor copilot"; strings.Join(names, " ") != want {
		t.Errorf("providers = %v, want %s", names, want)
	}
}

func TestAllOverrides(t *testing.T) {
	override := []Model{{ID: "custom-1", Display: "Custom"}}
	providers := All(map[string][]Model{"cursor": override})
	for _, p := range providers {
		if p.Name() != "cursor" {
			continue
		}
		if len(p.Models()) != 1 || p.Models()[0].ID != "custom-1" {
			t.Errorf("cursor models = %v, want the override only", p.Models())
		}
	}
}

func TestCosts(t *testing.T) {
	model := Model{ID: "m", InputUSD: usd(3.0), OutputUSD: usd(15.0)}
	tokens := 1500
	if got := InputCostUSD(model, &tokens); got == nil || *got != 0.0045 {
		t.Errorf("InputCostUSD = %v, want 0.0045", got)
	}
	if got := InputCostUSD(Model{ID: "unpriced"}, &tokens); got != nil {
		t.Errorf("unpriced InputCostUSD = %v, want nil", *got)
	}
	in, out := 1_000_000, 100_000
	if got := UsageCostUSD(model, Usage{InputTokens: &in, OutputTokens: &out}); got == nil || *got != 4.5 {
		t.Errorf("UsageCostUSD = %v, want 4.5", got)
	}
}
