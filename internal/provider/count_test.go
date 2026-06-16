// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestCountTokensGoogle(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "g-key")
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if got := r.Header.Get("x-goog-api-key"); got != "g-key" {
			t.Errorf("x-goog-api-key = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]int{"totalTokens": 11})
	}))
	defer srv.Close()

	g := NewGoogle()
	g.CountURLBase = srv.URL + "/models/"
	g.Client = srv.Client()
	got, err := g.CountTokens(context.Background(), "gemini-3.5-flash", "hi")
	if err != nil || got != 11 {
		t.Errorf("CountTokens = %d, %v; want 11, nil", got, err)
	}
	if gotPath != "/models/gemini-3.5-flash:countTokens" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestCountTokensHTTPError(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "o-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	o := NewOpenAI()
	o.CountURL = srv.URL
	o.Client = srv.Client()
	if _, err := o.CountTokens(context.Background(), "gpt-5.5", "x"); err == nil {
		t.Error("want error on HTTP 429, got nil")
	}
}

func TestCursorHasNoCounting(t *testing.T) {
	var p Provider = NewCursor()
	if _, ok := p.(TokenCounter); ok {
		t.Error("cursor must not implement TokenCounter")
	}
	if _, ok := p.(EvalRunner); !ok {
		t.Error("cursor must implement EvalRunner")
	}
}
