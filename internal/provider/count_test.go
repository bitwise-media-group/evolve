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
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	if _, err := NewAnthropic().CountTokens(context.Background(), "m", "x"); !errors.Is(err, ErrNoCredential) {
		t.Errorf("err = %v, want ErrNoCredential", err)
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
	if _, ok := p.(CaseRunner); !ok {
		t.Error("cursor must implement CaseRunner")
	}
}
