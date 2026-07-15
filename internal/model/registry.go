// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package model

import "slices"

// Provider ids. Cursor is both a provider (it owns Composer) and a harness
// (its CLI runs Composer and other vendors' models); the two ids living in
// different namespaces is intentional. xAI owns Grok models; the Grok CLI is
// the harness that drives them.
const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
	ProviderGoogle    = "google"
	ProviderCursor    = "cursor"
	ProviderXAI       = "xai"
)

// Harness ids, referenced by the Supported maps below. The harness package
// owns the Harness implementations; these constants are the shared vocabulary.
const (
	HarnessClaude      = "claude"
	HarnessCodex       = "codex"
	HarnessGemini      = "gemini"
	HarnessCursor      = "cursor"
	HarnessCopilot     = "copilot"
	HarnessAntigravity = "antigravity"
	HarnessGrok        = "grok"
)

// Providers returns the model vendors in display order.
func Providers() []Provider {
	return []Provider{
		{ID: ProviderAnthropic, Name: "Anthropic"},
		{ID: ProviderOpenAI, Name: "OpenAI"},
		{ID: ProviderGoogle, Name: "Google"},
		{ID: ProviderCursor, Name: "Cursor"},
		{ID: ProviderXAI, Name: "xAI"},
	}
}

// builtins returns the canonical model registry: one entry per vendor model,
// each declaring which harnesses can drive it and the CLI-specific id each
// harness uses. A model previously listed under a non-vendor harness (Copilot's
// "claude-sonnet-4.6", Antigravity's "gemini-3.5-flash") is folded into its
// canonical vendor model as an extra Supported entry.
func builtins() []Model {
	return []Model{
		// Anthropic — driven by Claude Code, all also by Copilot.
		{
			ID: "anthropic/claude-haiku-4-5", ProviderID: ProviderAnthropic, Name: "Claude Haiku 4.5",
			InputUSD: usd(1.00), OutputUSD: usd(5.00),
			Supported: map[string]string{HarnessClaude: "claude-haiku-4-5", HarnessCopilot: "claude-haiku-4.5"},
			Preferred: HarnessClaude,
		},
		{
			ID: "anthropic/claude-sonnet-4-6", ProviderID: ProviderAnthropic, Name: "Claude Sonnet 4.6",
			InputUSD: usd(3.00), OutputUSD: usd(15.00),
			Supported: map[string]string{HarnessClaude: "claude-sonnet-4-6", HarnessCopilot: "claude-sonnet-4.6"},
			Preferred: HarnessClaude,
		},
		{
			// Sticker rate; an introductory $2/$10 per MTok applies through 2026-08-31.
			ID: "anthropic/claude-sonnet-5", ProviderID: ProviderAnthropic, Name: "Claude Sonnet 5",
			InputUSD: usd(3.00), OutputUSD: usd(15.00),
			Supported: map[string]string{HarnessClaude: "claude-sonnet-5", HarnessCopilot: "claude-sonnet-5"},
			Preferred: HarnessClaude,
		},
		{
			ID: "anthropic/claude-opus-4-8", ProviderID: ProviderAnthropic, Name: "Claude Opus 4.8",
			InputUSD: usd(5.00), OutputUSD: usd(25.00),
			Supported: map[string]string{HarnessClaude: "claude-opus-4-8", HarnessCopilot: "claude-opus-4.8"},
			Preferred: HarnessClaude,
		},
		{
			ID: "anthropic/claude-fable-5", ProviderID: ProviderAnthropic, Name: "Claude Fable 5",
			InputUSD: usd(10.00), OutputUSD: usd(50.00),
			Supported: map[string]string{HarnessClaude: "claude-fable-5", HarnessCopilot: "claude-fable-5"},
			Preferred: HarnessClaude,
		},

		// OpenAI — Codex models priced; the Copilot-only ids carry no published
		// per-token pricing (subscription billing), so estimate/measured render n/a.
		{
			ID: "openai/gpt-5.6-sol", ProviderID: ProviderOpenAI, Name: "GPT-5.6 Sol",
			InputUSD: usd(5.00), OutputUSD: usd(30.00),
			Supported: map[string]string{HarnessCodex: "gpt-5.6-sol", HarnessCopilot: "gpt-5.6-sol"},
			Preferred: HarnessCodex,
		},
		{
			ID: "openai/gpt-5.6-terra", ProviderID: ProviderOpenAI, Name: "GPT-5.6 Terra",
			InputUSD: usd(2.50), OutputUSD: usd(15.00),
			Supported: map[string]string{HarnessCodex: "gpt-5.6-terra", HarnessCopilot: "gpt-5.6-terra"},
			Preferred: HarnessCodex,
		},
		{
			ID: "openai/gpt-5.6-luna", ProviderID: ProviderOpenAI, Name: "GPT-5.6 Luna",
			InputUSD: usd(1.00), OutputUSD: usd(6.00),
			Supported: map[string]string{HarnessCodex: "gpt-5.6-luna", HarnessCopilot: "gpt-5.6-luna"},
			Preferred: HarnessCodex,
		},
		{
			ID: "openai/gpt-5.3-codex-spark", ProviderID: ProviderOpenAI, Name: "GPT-5.3 Codex Spark",
			Supported: map[string]string{HarnessCodex: "gpt-5.3-codex-spark"},
			Preferred: HarnessCodex,
		},
		{
			ID: "openai/gpt-5.4-mini", ProviderID: ProviderOpenAI, Name: "GPT-5.4 Mini",
			InputUSD: usd(0.75), OutputUSD: usd(4.50),
			Supported: map[string]string{HarnessCodex: "gpt-5.4-mini", HarnessCopilot: "gpt-5.4-mini"},
			Preferred: HarnessCodex,
		},
		{
			ID: "openai/gpt-5.4", ProviderID: ProviderOpenAI, Name: "GPT-5.4",
			InputUSD: usd(2.50), OutputUSD: usd(15.00),
			Supported: map[string]string{HarnessCodex: "gpt-5.4", HarnessCopilot: "gpt-5.4"},
			Preferred: HarnessCodex,
		},
		{
			ID: "openai/gpt-5.5", ProviderID: ProviderOpenAI, Name: "GPT-5.5",
			InputUSD: usd(5.00), OutputUSD: usd(30.00),
			Supported: map[string]string{HarnessCodex: "gpt-5.5", HarnessCopilot: "gpt-5.5"},
			Preferred: HarnessCodex,
		},
		{
			ID: "openai/gpt-5.3-codex", ProviderID: ProviderOpenAI, Name: "GPT-5.3 Codex",
			Supported: map[string]string{HarnessCopilot: "gpt-5.3-codex"},
			Preferred: HarnessCopilot,
		},

		// Google — Gemini models priced; the id with no Gemini CLI counterpart
		// carries no published per-token pricing (quota/subscription billing).
		{
			ID: "google/gemini-3.1-flash-lite", ProviderID: ProviderGoogle, Name: "Gemini 3.1 Flash-Lite",
			InputUSD: usd(0.25), OutputUSD: usd(1.50),
			Supported: map[string]string{HarnessGemini: "gemini-3.1-flash-lite"},
			Preferred: HarnessGemini,
		},
		{
			ID: "google/gemini-3-flash-preview", ProviderID: ProviderGoogle, Name: "Gemini 3 Flash (preview)",
			InputUSD: usd(0.50), OutputUSD: usd(3.00),
			Supported: map[string]string{HarnessGemini: "gemini-3-flash-preview"},
			Preferred: HarnessGemini,
		},
		{
			ID: "google/gemini-3.5-flash", ProviderID: ProviderGoogle, Name: "Gemini 3.5 Flash",
			InputUSD: usd(1.50), OutputUSD: usd(9.00),
			Supported: map[string]string{
				HarnessGemini:      "gemini-3.5-flash",
				HarnessAntigravity: "gemini-3.5-flash",
				HarnessCopilot:     "gemini-3.5-flash",
			},
			Preferred: HarnessGemini,
		},
		{
			// <=200K-token tier; long-context requests price higher.
			ID: "google/gemini-3.1-pro-preview", ProviderID: ProviderGoogle, Name: "Gemini 3.1 Pro (preview)",
			InputUSD: usd(2.00), OutputUSD: usd(12.00),
			Supported: map[string]string{HarnessGemini: "gemini-3.1-pro-preview"},
			Preferred: HarnessGemini,
		},
		{
			ID: "google/gemini-3.1-pro", ProviderID: ProviderGoogle, Name: "Gemini 3.1 Pro",
			Supported: map[string]string{HarnessAntigravity: "gemini-3.1-pro", HarnessCopilot: "gemini-3.1-pro"},
			Preferred: HarnessAntigravity,
		},

		// Cursor — Composer is Cursor's own model. The Cursor CLI is preferred;
		// the Grok CLI also serves it under a diverging id (grok-composer-2.5-fast).
		// No counting API and no Cursor-CLI usage reporting, so figures render n/a
		// when driven by Cursor; Grok reports usage when it is the runner.
		{
			ID: "cursor/composer-2.5", ProviderID: ProviderCursor, Name: "Cursor Composer 2.5",
			Supported: map[string]string{
				HarnessCursor: "composer-2.5",
				HarnessGrok:   "grok-composer-2.5-fast",
			},
			Preferred: HarnessCursor,
		},

		// xAI — driven by the Grok CLI. Pricing is the short-context sticker rate.
		{
			ID: "xai/grok-4.5", ProviderID: ProviderXAI, Name: "Grok 4.5",
			InputUSD: usd(2.00), OutputUSD: usd(6.00),
			Supported: map[string]string{HarnessGrok: "grok-4.5"},
			Preferred: HarnessGrok,
		},
	}
}

// AllModels returns the canonical model registry with any per-provider config
// override applied. overrides maps a provider id to a replacement model list
// (replace, not merge — partial merges create "which price won?" ambiguity);
// only models whose ProviderID is overridden are replaced.
func AllModels(overrides map[string][]Model) []Model {
	if len(overrides) == 0 {
		return builtins()
	}
	var out []Model
	for _, m := range builtins() {
		if _, ok := overrides[m.ProviderID]; ok {
			continue // replaced below
		}
		out = append(out, m)
	}
	for _, p := range Providers() {
		if models, ok := overrides[p.ID]; ok {
			out = append(out, models...)
		}
	}
	return out
}

// ModelByID returns the model with the given canonical id from models, if any.
func ModelByID(models []Model, id string) (Model, bool) {
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// ProviderByID returns the vendor with the given id, if any.
func ProviderByID(id string) (Provider, bool) {
	for _, p := range Providers() {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// providerIDs is the set of known vendor ids, used to validate override keys.
func providerIDs() []string {
	ids := make([]string, 0, 5)
	for _, p := range Providers() {
		ids = append(ids, p.ID)
	}
	return ids
}

// IsProviderID reports whether id names a known vendor.
func IsProviderID(id string) bool { return slices.Contains(providerIDs(), id) }
