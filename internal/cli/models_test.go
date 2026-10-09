// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"

	"github.com/spf13/viper"
)

func TestModelsSpec(t *testing.T) {
	o := &Options{Viper: viper.New()}
	if got := o.ModelsSpec("anthropic"); got != "anthropic" {
		t.Errorf("flag: %q, want anthropic", got)
	}
	if got := o.ModelsSpec(""); got != "all" {
		t.Errorf("unset: %q, want all", got)
	}
	o.Viper.Set("models", []string{"anthropic/claude-sonnet-4-6", "openai/gpt-5.4"})
	if got := o.ModelsSpec(""); got != "anthropic/claude-sonnet-4-6,openai/gpt-5.4" {
		t.Errorf("config: %q", got)
	}
}

func TestConfiguredModels(t *testing.T) {
	o := &Options{Viper: viper.New()}
	all, err := o.ConfiguredModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 5 {
		t.Fatalf("unrestricted = %d models, want the full registry", len(all))
	}

	o.Viper.Set("models", []string{"anthropic"})
	restricted, err := o.ConfiguredModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(restricted) == 0 || len(restricted) >= len(all) {
		t.Fatalf("restricted = %d, want fewer than %d", len(restricted), len(all))
	}
	for _, m := range restricted {
		if m.ProviderID != "anthropic" {
			t.Errorf("restricted model %q is not anthropic", m.ID)
		}
	}
}

func TestActiveModelKeysHarnessRestriction(t *testing.T) {
	// A harnesses restriction alone makes ActiveModelKeys "configured" and limits
	// the keys to models a configured harness can drive.
	o := &Options{Viper: viper.New()}
	o.Viper.Set("harnesses", []string{"codex"})
	keys, configured, err := o.ActiveModelKeys()
	if err != nil {
		t.Fatal(err)
	}
	if !configured {
		t.Fatal("configured = false, want true when harnesses is set")
	}
	if keys["anthropic/claude-opus-4-8"] {
		t.Error("opus (claude-only) must not be active under a codex-only restriction")
	}
	if !keys["openai/gpt-6-sol"] {
		t.Error("gpt-6-sol (codex) must be active under a codex restriction")
	}
}
