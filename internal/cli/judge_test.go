// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// stubPath points PATH at a temp dir holding executable stubs for the named
// CLIs, so harness availability is fully test-controlled.
func stubPath(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestJudgeSelection(t *testing.T) {
	tests := []struct {
		name         string
		path         []string
		cfgHarnesses []string
		cfgModels    []string
		token        string
		wantModel    string
		wantHarness  string
		wantErr      string
	}{
		{"default token", []string{"claude"}, nil, nil, "",
			"anthropic/claude-sonnet-5", "claude", ""},
		{"bare id", []string{"claude"}, nil, nil, "claude-sonnet-5",
			"anthropic/claude-sonnet-5", "claude", ""},
		{"canonical id", []string{"claude"}, nil, nil, "anthropic/claude-sonnet-5",
			"anthropic/claude-sonnet-5", "claude", ""},
		{"unknown token", []string{"claude"}, nil, nil, "bogus",
			"", "", "not a known model"},
		// The judge is a grading instrument: a `models` restriction on what is
		// under test does not constrain it.
		{"models restriction ignored", []string{"claude"}, nil, []string{"openai"}, "claude-sonnet-5",
			"anthropic/claude-sonnet-5", "claude", ""},
		// With claude missing, the same judge model runs via the next supported
		// installed harness instead of failing.
		{"preference fallback", []string{"copilot"}, nil, nil, "claude-sonnet-5",
			"anthropic/claude-sonnet-5", "copilot", ""},
		{"no harness installed", nil, nil, nil, "claude-sonnet-5",
			"", "", "no installed harness can run judge sessions"},
		{"harnesses restriction respected", []string{"claude", "copilot"}, []string{"codex"}, nil, "claude-sonnet-5",
			"", "", "no installed harness can run judge sessions"},
		// Gemini implements no EvalRunner, so a gemini-only model cannot judge
		// even with the CLI installed.
		{"gemini only", []string{"gemini"}, nil, nil, "google/gemini-3.1-flash-lite",
			"", "", "no headless eval support yet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubPath(t, tt.path...)
			o := &Options{Viper: viper.New()}
			if tt.cfgHarnesses != nil {
				o.Viper.Set("harnesses", tt.cfgHarnesses)
			}
			if tt.cfgModels != nil {
				o.Viper.Set("models", tt.cfgModels)
			}
			sel, err := o.JudgeSelection(tt.token)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if sel.Model.ID != tt.wantModel || sel.Harness.ID() != tt.wantHarness {
				t.Errorf("selection = (%s, %s), want (%s, %s)",
					sel.Model.ID, sel.Harness.ID(), tt.wantModel, tt.wantHarness)
			}
		})
	}
}
