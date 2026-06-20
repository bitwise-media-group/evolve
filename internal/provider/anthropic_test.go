// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeErrorAnthropic(t *testing.T) {
	a := NewAnthropic()
	success := []byte(`{"type":"result","subtype":"success","is_error":false,"result":"done"}`)
	maxTurns := []byte(`{"type":"result","subtype":"error_max_turns","is_error":true,"result":"partial work"}`)
	errEnvelope := []byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":""}`)
	cases := []struct {
		name    string
		stdout  []byte
		exit    int
		wantErr bool
	}{
		{"empty", []byte("  \n"), 1, true},
		{"success exit 0", success, 0, false},
		{"success with non-zero exit", success, 1, false}, // a result exists — grade it, never error
		{"max-turns partial", maxTurns, 1, false},
		{"error envelope, empty result", errEnvelope, 1, true},
		{"garbage, clean exit", []byte("not json"), 0, false},
		{"garbage, non-zero exit", []byte("not json"), 1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := a.RuntimeError(c.stdout, c.exit, false); (got != "") != c.wantErr {
				t.Errorf("RuntimeError(%q, %d) = %q, wantErr=%v", c.stdout, c.exit, got, c.wantErr)
			}
		})
	}
}

// TestRuntimeErrorAnthropicDetail asserts the reason lifts the failure detail
// the claude CLI reports only on stdout — the subtype and the `errors` array —
// since it never writes either to stderr. Payload mirrors a real error_max_turns
// result envelope (no `result` field, is_error=true).
func TestRuntimeErrorAnthropicDetail(t *testing.T) {
	a := NewAnthropic()
	stdout := []byte(`{"type":"result","subtype":"error_max_turns","is_error":true,` +
		`"num_turns":21,"errors":["Reached maximum number of turns (20)"]}`)
	got := a.RuntimeError(stdout, 1, false)
	for _, want := range []string{"error_max_turns", "Reached maximum number of turns (20)"} {
		if !strings.Contains(got, want) {
			t.Errorf("RuntimeError reason %q missing %q", got, want)
		}
	}

	// Multiple errors join into one line; an empty errors array degrades to the
	// subtype-only reason.
	multi := []byte(`{"type":"result","subtype":"error_during_execution","is_error":true,` +
		`"errors":["first failure","second failure"]}`)
	if got := a.RuntimeError(multi, 1, false); !strings.Contains(got, "first failure; second failure") {
		t.Errorf("RuntimeError reason %q should join multiple errors", got)
	}
	bare := []byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"errors":[]}`)
	if got := a.RuntimeError(bare, 1, false); got != "claude run error (error_during_execution)" {
		t.Errorf("RuntimeError reason = %q, want subtype-only", got)
	}
}

func TestScanLineAnthropic(t *testing.T) {
	a := NewAnthropic()
	tests := []struct {
		name     string
		line     string
		skill    string
		wantHit  bool
		wantNote string
	}{
		{
			name:    "Skill tool",
			line:    `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"go-testing"}}]}}`,
			skill:   "go-testing",
			wantHit: true,
		},
		{
			name:    "Read of SKILL.md",
			line:    `{"message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/ws/.claude/skills/go-testing/SKILL.md"}}]}}`,
			skill:   "go-testing",
			wantHit: true,
		},
		{
			name:    "Read of sibling skill",
			line:    `{"message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/ws/.claude/skills/go-style/SKILL.md"}}]}}`,
			skill:   "go-testing",
			wantHit: false,
		},
		{
			name:    "text block mentioning skill",
			line:    `{"message":{"content":[{"type":"text","text":"I could use go-testing skills/go-testing/SKILL.md"}]}}`,
			skill:   "go-testing",
			wantHit: false,
		},
		{
			name:    "non-JSON noise",
			line:    `warning: something`,
			skill:   "go-testing",
			wantHit: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit, note := a.ScanLine([]byte(tt.line), tt.skill)
			if hit != tt.wantHit {
				t.Errorf("hit = %v, want %v", hit, tt.wantHit)
			}
			if note != tt.wantNote {
				t.Errorf("note = %q, want %q", note, tt.wantNote)
			}
		})
	}
}

func TestParseEvalOutputAnthropic(t *testing.T) {
	stdout := `{"result":"done","usage":{"input_tokens":10,"cache_creation_input_tokens":5,"cache_read_input_tokens":85,"output_tokens":42},"total_cost_usd":0.12}`
	text, usage := NewAnthropic().ParseEvalOutput([]byte(stdout))
	if text != "done" {
		t.Errorf("text = %q, want done", text)
	}
	// Input is fresh-only; cache reads/writes stay on their own fields rather
	// than folding into input.
	if usage == nil || *usage.InputTokens != 10 || *usage.CacheReadTokens != 85 ||
		*usage.CacheCreationTokens != 5 || *usage.OutputTokens != 42 || *usage.CostUSD != 0.12 {
		t.Errorf("usage = %+v, want input=10 cacheRead=85 cacheCreation=5 output=42 cost=0.12", usage)
	}

	text, usage = NewAnthropic().ParseEvalOutput([]byte("not json at all"))
	if text != "not json at all" || usage != nil {
		t.Errorf("unparseable: text=%q usage=%v, want raw stdout and nil", text, usage)
	}
}

func TestCountTokensAnthropic(t *testing.T) {
	t.Setenv("EVOLVE_ANTHROPIC_API_KEY", "")
	t.Setenv("EVOLVE_CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key = %q", got)
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "claude-fable-5" || body.Messages[0].Content != "hello" {
			t.Errorf("body = %+v", body)
		}
		json.NewEncoder(w).Encode(map[string]int{"input_tokens": 7})
	}))
	defer srv.Close()

	a := NewAnthropic()
	a.CountURL = srv.URL
	a.Client = srv.Client()
	got, err := a.CountTokens(context.Background(), "claude-fable-5", "hello")
	if err != nil || got != 7 {
		t.Errorf("CountTokens = %d, %v; want 7, nil", got, err)
	}
}

func TestCountTokensAnthropicOAuth(t *testing.T) {
	t.Setenv("EVOLVE_ANTHROPIC_API_KEY", "")
	t.Setenv("EVOLVE_CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "oauth-tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer oauth-tok" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != "oauth-2025-04-20" {
			t.Errorf("anthropic-beta = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]int{"input_tokens": 3})
	}))
	defer srv.Close()

	a := NewAnthropic()
	a.CountURL = srv.URL
	a.Client = srv.Client()
	if got, err := a.CountTokens(context.Background(), "m", "x"); err != nil || got != 3 {
		t.Errorf("CountTokens = %d, %v; want 3, nil", got, err)
	}
}

func TestCountTokensNoCredential(t *testing.T) {
	t.Setenv("EVOLVE_ANTHROPIC_API_KEY", "")
	t.Setenv("EVOLVE_CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	if _, err := NewAnthropic().CountTokens(context.Background(), "m", "x"); !errors.Is(err, ErrNoCredential) {
		t.Errorf("err = %v, want ErrNoCredential", err)
	}
}

// TestCountTokensAnthropicProprietary pins that EVOLVE_ANTHROPIC_API_KEY takes
// priority over the provider's own vars and is sent as an API key (x-api-key),
// not an OAuth bearer token.
func TestCountTokensAnthropicProprietary(t *testing.T) {
	t.Setenv("EVOLVE_ANTHROPIC_API_KEY", "evolve-key")
	t.Setenv("ANTHROPIC_API_KEY", "provider-key")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "oauth-tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "evolve-key" {
			t.Errorf("x-api-key = %q, want the proprietary key sent first as an API key", got)
		}
		if got := r.Header.Get("authorization"); got != "" {
			t.Errorf("authorization = %q, want no bearer header for an API key", got)
		}
		json.NewEncoder(w).Encode(map[string]int{"input_tokens": 5})
	}))
	defer srv.Close()

	a := NewAnthropic()
	a.CountURL = srv.URL
	a.Client = srv.Client()
	if got, err := a.CountTokens(context.Background(), "m", "x"); err != nil || got != 5 {
		t.Errorf("CountTokens = %d, %v; want 5, nil", got, err)
	}
}

// TestCountTokensAnthropicProprietaryOAuth pins that EVOLVE_CLAUDE_CODE_OAUTH_TOKEN
// is sent as an OAuth bearer token (with the oauth beta header), not an API key,
// so an OAuth credential that 401s as x-api-key works here.
func TestCountTokensAnthropicProprietaryOAuth(t *testing.T) {
	t.Setenv("EVOLVE_ANTHROPIC_API_KEY", "")
	t.Setenv("EVOLVE_CLAUDE_CODE_OAUTH_TOKEN", "evolve-oauth")
	t.Setenv("ANTHROPIC_API_KEY", "provider-key")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "provider-oauth")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer evolve-oauth" {
			t.Errorf("authorization = %q, want the proprietary OAuth token as a bearer (highest priority)", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != "oauth-2025-04-20" {
			t.Errorf("anthropic-beta = %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Errorf("x-api-key = %q, want no API-key header for an OAuth token", got)
		}
		json.NewEncoder(w).Encode(map[string]int{"input_tokens": 4})
	}))
	defer srv.Close()

	a := NewAnthropic()
	a.CountURL = srv.URL
	a.Client = srv.Client()
	if got, err := a.CountTokens(context.Background(), "m", "x"); err != nil || got != 4 {
		t.Errorf("CountTokens = %d, %v; want 4, nil", got, err)
	}
}
