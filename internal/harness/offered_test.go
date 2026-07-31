// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/model"
)

// TestOfferedModelsCapability pins which harnesses can report the operator's
// offered models: Claude (client-side /model alias list), Codex (app-server
// model/list), and Grok (`grok models`). The rest have no listing surface, so
// their models always count as offered (fail open).
func TestOfferedModelsCapability(t *testing.T) {
	want := map[string]bool{
		model.HarnessClaude: true, model.HarnessCodex: true, model.HarnessGemini: false,
		model.HarnessCursor: false, model.HarnessCopilot: false, model.HarnessAntigravity: false,
		model.HarnessGrok: true,
	}
	for _, h := range All() {
		_, isLister := h.(OfferedModels)
		if isLister != want[h.ID()] {
			t.Errorf("%s OfferedModels = %v, want %v", h.ID(), isLister, want[h.ID()])
		}
	}
}

func TestOffersModel(t *testing.T) {
	m := model.Model{
		ID: "anthropic/claude-sonnet-5", ProviderID: "anthropic", Name: "Claude Sonnet 5",
		Supported: map[string]string{"claude": "claude-sonnet-5", "copilot": "claude-sonnet-5"},
		Preferred: "claude",
	}
	cases := []struct {
		name   string
		hid    string
		tokens []string
		want   bool
	}{
		{"cli id exact", "claude", []string{"claude-sonnet-5"}, true},
		{"cli id case-insensitive", "claude", []string{"Claude-Sonnet-5"}, true},
		{"full display name", "claude", []string{"Claude Sonnet 5"}, true},
		{"bare display name suffix", "claude", []string{"Sonnet 5"}, true},
		{"different model name", "claude", []string{"Sonnet 4.6", "Opus 5"}, false},
		{"substring is not a word suffix", "claude", []string{"onnet 5"}, false},
		{"empty tokens", "claude", nil, false},
		{"unsupported harness id matches by name only", "codex", []string{"Sonnet 5"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := OffersModel(m, tc.hid, tc.tokens); got != tc.want {
				t.Errorf("OffersModel(%v) = %v, want %v", tc.tokens, got, tc.want)
			}
		})
	}
}

// claudeModelUsage is the client-side `claude -p "/model"` output the alias
// parser is pinned to (claude 2.1.220).
const claudeModelUsage = `Current model: Fable 5 (effort: high)
Usage: /model <name>. Available: sonnet, opus, haiku, fable, best, sonnet[1m], opus[1m], fable[1m], opusplan, default, or a full model ID.
`

func TestClaudeListOfferedModels(t *testing.T) {
	resolutions := map[string]string{
		"sonnet": "Current model: Sonnet 5 (effort: high)\nUsage: ...",
		"opus":   "Current model: Opus 5 (effort: high)\nUsage: ...",
		"haiku":  "Current model: Haiku 4.5 (effort: high)\nUsage: ...",
		"fable":  "Current model: Fable 5 (effort: high)\nUsage: ...",
	}
	exec := func(_ context.Context, spec model.CommandSpec, done func([]byte) bool) ([]byte, error) {
		if done != nil {
			t.Error("claude probe passed a done predicate; its CLI exits on its own")
		}
		if len(spec.Argv) == 3 { // claude -p /model
			return []byte(claudeModelUsage), nil
		}
		alias := spec.Argv[len(spec.Argv)-1]
		out, ok := resolutions[alias]
		if !ok {
			t.Errorf("unexpected alias resolved: %q", alias)
		}
		return []byte(out), nil
	}

	offered, err := NewClaude().ListOfferedModels(t.Context(), exec)
	if err != nil {
		t.Fatalf("ListOfferedModels: %v", err)
	}
	want := []string{"Sonnet 5", "Opus 5", "Haiku 4.5", "Fable 5"}
	if !slices.Equal(offered, want) {
		t.Errorf("offered = %v, want %v", offered, want)
	}
}

func TestClaudeListOfferedModelsUnparseable(t *testing.T) {
	exec := func(context.Context, model.CommandSpec, func([]byte) bool) ([]byte, error) {
		return []byte("You need to log in first.\n"), nil
	}
	offered, err := NewClaude().ListOfferedModels(t.Context(), exec)
	if err != nil || offered != nil {
		t.Errorf("unparseable output = (%v, %v), want (nil, nil) — unknown fails open", offered, err)
	}
}

func TestClaudeListOfferedModelsProbeError(t *testing.T) {
	exec := func(context.Context, model.CommandSpec, func([]byte) bool) ([]byte, error) {
		return nil, errors.New("boom")
	}
	if _, err := NewClaude().ListOfferedModels(t.Context(), exec); err == nil {
		t.Error("probe error swallowed, want propagated")
	}
}

// codexModelListResponse is an app-server model/list response (id 2) shaped
// per the protocol's generated ModelListResponse schema (codex 0.146.0).
const codexModelListResponse = `{"id":2,"result":{"data":[` +
	`{"id":"gpt-5.6-sol","model":"gpt-5.6-sol","displayName":"gpt-5.6-sol","hidden":false,"isDefault":true},` +
	`{"id":"gpt-5.4","model":"gpt-5.4","displayName":"GPT-5.4","hidden":false,"isDefault":false}]}}`

func TestCodexListOfferedModels(t *testing.T) {
	exec := func(_ context.Context, spec model.CommandSpec, done func([]byte) bool) ([]byte, error) {
		if done == nil {
			t.Fatal("codex probe must pass a done predicate; app-server never exits on its own")
		}
		stdin := string(spec.Stdin)
		for _, method := range []string{`"initialize"`, `"initialized"`, `"model/list"`} {
			if !strings.Contains(stdin, method) {
				t.Errorf("probe stdin missing %s request", method)
			}
		}
		var out []byte
		for line := range strings.Lines(`{"id":1,"result":{"userAgent":"codex"}}` + "\n" +
			codexModelListResponse + "\n") {
			out = append(out, line...)
			if done([]byte(line)) {
				return out, nil
			}
		}
		t.Fatal("done predicate never fired on the model/list response")
		return nil, nil
	}

	offered, err := NewCodex().ListOfferedModels(t.Context(), exec)
	if err != nil {
		t.Fatalf("ListOfferedModels: %v", err)
	}
	want := []string{"gpt-5.6-sol", "gpt-5.4", "GPT-5.4"}
	if !slices.Equal(offered, want) {
		t.Errorf("offered = %v, want %v", offered, want)
	}
}

func TestCodexListOfferedModelsNoResponse(t *testing.T) {
	exec := func(context.Context, model.CommandSpec, func([]byte) bool) ([]byte, error) {
		return []byte(`{"id":1,"result":{"userAgent":"codex"}}` + "\n"), nil
	}
	offered, err := NewCodex().ListOfferedModels(t.Context(), exec)
	if err != nil || offered != nil {
		t.Errorf("missing response = (%v, %v), want (nil, nil) — unknown fails open", offered, err)
	}
}

const grokModelsOutput = `You are logged in with grok.com.

Default model: grok-4.5

Available models:
  * grok-4.5 (default)
  * grok-composer-2.5-fast
`

func TestGrokListOfferedModels(t *testing.T) {
	exec := func(_ context.Context, spec model.CommandSpec, done func([]byte) bool) ([]byte, error) {
		if done != nil {
			t.Error("grok probe passed a done predicate; its CLI exits on its own")
		}
		if want := []string{"grok", "models"}; !slices.Equal(spec.Argv, want) {
			t.Errorf("argv = %v, want %v", spec.Argv, want)
		}
		return []byte(grokModelsOutput), nil
	}
	offered, err := NewGrok().ListOfferedModels(t.Context(), exec)
	if err != nil {
		t.Fatalf("ListOfferedModels: %v", err)
	}
	want := []string{"grok-4.5", "grok-composer-2.5-fast"}
	if !slices.Equal(offered, want) {
		t.Errorf("offered = %v, want %v", offered, want)
	}
}
