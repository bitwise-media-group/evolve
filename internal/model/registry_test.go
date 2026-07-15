// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package model

import (
	"strings"
	"testing"
)

// TestBuiltinInvariants guards the registry's structural rules: ids are
// provider-qualified, the ProviderID prefix matches the id, every model is
// supported by at least one harness, and Preferred is always one of those
// harnesses. A bad Supported/Preferred entry would silently send the wrong
// --model string to a CLI, so these are load-bearing.
func TestBuiltinInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range builtins() {
		if seen[m.ID] {
			t.Errorf("duplicate model id %q", m.ID)
		}
		seen[m.ID] = true

		prov, rest, ok := strings.Cut(m.ID, "/")
		if !ok || rest == "" {
			t.Errorf("model id %q is not provider-qualified", m.ID)
		}
		if prov != m.ProviderID {
			t.Errorf("model %q: id prefix %q != ProviderID %q", m.ID, prov, m.ProviderID)
		}
		if !IsProviderID(m.ProviderID) {
			t.Errorf("model %q: unknown ProviderID %q", m.ID, m.ProviderID)
		}
		if len(m.Supported) == 0 {
			t.Errorf("model %q: no supported harness", m.ID)
		}
		if _, ok := m.Supported[m.Preferred]; !ok {
			t.Errorf("model %q: Preferred %q not in Supported %v", m.ID, m.Preferred, m.Supported)
		}
	}
}

// TestKeyStability pins the results-key bytes for a vendor-native model so the
// harness split cannot orphan committed results.
func TestKeyStability(t *testing.T) {
	m, ok := ModelByID(builtins(), "anthropic/claude-sonnet-4-6")
	if !ok {
		t.Fatal("anthropic/claude-sonnet-4-6 missing from registry")
	}
	if m.Key() != "anthropic/claude-sonnet-4-6" {
		t.Errorf("Key() = %q, want anthropic/claude-sonnet-4-6", m.Key())
	}
	if m.BareID() != "claude-sonnet-4-6" {
		t.Errorf("BareID() = %q, want claude-sonnet-4-6", m.BareID())
	}
	// Copilot drives the same model under a different CLI id; the divergence
	// lives only in the Supported map, never in the key.
	if id, ok := m.CLIModelID("copilot"); !ok || id != "claude-sonnet-4.6" {
		t.Errorf("copilot CLI id = %q (%v), want claude-sonnet-4.6", id, ok)
	}
	if id, ok := m.CLIModelID("claude"); !ok || id != "claude-sonnet-4-6" {
		t.Errorf("claude CLI id = %q (%v), want claude-sonnet-4-6", id, ok)
	}
}

func TestGPT56Models(t *testing.T) {
	tests := []struct {
		id, name      string
		input, output float64
	}{
		{"openai/gpt-5.6-sol", "GPT-5.6 Sol", 5.00, 30.00},
		{"openai/gpt-5.6-terra", "GPT-5.6 Terra", 2.50, 15.00},
		{"openai/gpt-5.6-luna", "GPT-5.6 Luna", 1.00, 6.00},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, ok := ModelByID(builtins(), tc.id)
			if !ok {
				t.Fatalf("%s missing from registry", tc.id)
			}
			if m.Name != tc.name {
				t.Errorf("Name = %q, want %q", m.Name, tc.name)
			}
			if m.InputUSD == nil || *m.InputUSD != tc.input || m.OutputUSD == nil || *m.OutputUSD != tc.output {
				t.Errorf("pricing = %v/%v, want %.2f/%.2f", m.InputUSD, m.OutputUSD, tc.input, tc.output)
			}
			for _, harnessID := range []string{HarnessCodex, HarnessCopilot} {
				if id, ok := m.CLIModelID(harnessID); !ok || id != m.BareID() {
					t.Errorf("%s CLI id = %q (%v), want %q", harnessID, id, ok, m.BareID())
				}
			}
			if m.Preferred != HarnessCodex {
				t.Errorf("Preferred = %q, want %q", m.Preferred, HarnessCodex)
			}
		})
	}
}

func TestGrok45Model(t *testing.T) {
	m, ok := ModelByID(builtins(), "xai/grok-4.5")
	if !ok {
		t.Fatal("xai/grok-4.5 missing from registry")
	}
	if m.Preferred != HarnessGrok {
		t.Errorf("Preferred = %q, want %q", m.Preferred, HarnessGrok)
	}
	if id, ok := m.CLIModelID(HarnessGrok); !ok || id != "grok-4.5" {
		t.Errorf("grok CLI id = %q (%v), want grok-4.5", id, ok)
	}
	if m.InputUSD == nil || *m.InputUSD != 2.00 || m.OutputUSD == nil || *m.OutputUSD != 6.00 {
		t.Errorf("pricing = %v/%v, want 2.00/6.00", m.InputUSD, m.OutputUSD)
	}
}

func TestComposer25GrokSupport(t *testing.T) {
	m, ok := ModelByID(builtins(), "cursor/composer-2.5")
	if !ok {
		t.Fatal("cursor/composer-2.5 missing from registry")
	}
	if id, ok := m.CLIModelID(HarnessGrok); !ok || id != "grok-composer-2.5-fast" {
		t.Errorf("grok CLI id = %q (%v), want grok-composer-2.5-fast", id, ok)
	}
	if m.Preferred != HarnessCursor {
		t.Errorf("Preferred = %q, want %q", m.Preferred, HarnessCursor)
	}
}

// TestAllModelsOverride replaces one provider's matrix and leaves the others.
func TestAllModelsOverride(t *testing.T) {
	override := map[string][]Model{
		ProviderCursor: {{
			ID: "cursor/composer-3", ProviderID: ProviderCursor, Name: "Composer 3",
			Supported: map[string]string{HarnessCursor: "composer-3"}, Preferred: HarnessCursor,
		}},
	}
	got := AllModels(override)
	if _, ok := ModelByID(got, "cursor/composer-2.5"); ok {
		t.Error("builtin cursor/composer-2.5 should be replaced by the override")
	}
	if _, ok := ModelByID(got, "cursor/composer-3"); !ok {
		t.Error("override cursor/composer-3 missing")
	}
	if _, ok := ModelByID(got, "anthropic/claude-sonnet-4-6"); !ok {
		t.Error("non-overridden anthropic models should remain")
	}
}
