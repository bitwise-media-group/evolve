// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package model

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListerFor(t *testing.T) {
	for _, pid := range []string{ProviderAnthropic, ProviderOpenAI, ProviderGoogle, ProviderXAI} {
		if _, ok := ListerFor(pid); !ok {
			t.Errorf("ListerFor(%q) = not found, want a lister", pid)
		}
	}
	// Cursor has no listing API; harness-only ids are not vendors.
	for _, pid := range []string{ProviderCursor, "copilot", "unknown"} {
		if _, ok := ListerFor(pid); ok {
			t.Errorf("ListerFor(%q) = found, want none", pid)
		}
	}
}

func TestAnthropicListerPaginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "k" {
			t.Errorf("x-api-key = %q, want k", got)
		}
		switch r.URL.Query().Get("after_id") {
		case "":
			fmt.Fprint(w, `{"data":[{"id":"claude-sonnet-5","display_name":"Claude Sonnet 5"}],"has_more":true,"last_id":"claude-sonnet-5"}`)
		case "claude-sonnet-5":
			fmt.Fprint(w, `{"data":[{"id":"claude-opus-4-8","display_name":"Claude Opus 4.8"}],"has_more":false}`)
		default:
			t.Errorf("unexpected after_id %q", r.URL.Query().Get("after_id"))
		}
	}))
	defer srv.Close()

	t.Setenv("EVOLVE_ANTHROPIC_API_KEY", "k")
	l := anthropicLister{url: srv.URL, envKeys: CounterEnvKeys(ProviderAnthropic), client: srv.Client()}
	got, err := l.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	want := []DiscoveredModel{
		{ID: "claude-sonnet-5", Name: "Claude Sonnet 5"},
		{ID: "claude-opus-4-8", Name: "Claude Opus 4.8"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d models, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("model[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestAnthropicListerNoCredential(t *testing.T) {
	for _, env := range CounterEnvKeys(ProviderAnthropic) {
		t.Setenv(env, "")
	}
	l := anthropicLister{url: "http://unused.invalid", envKeys: CounterEnvKeys(ProviderAnthropic)}
	if _, err := l.ListModels(context.Background()); err != ErrNoCredential {
		t.Errorf("err = %v, want ErrNoCredential", err)
	}
}

func TestOpenAILister(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer k" {
			t.Errorf("authorization = %q, want Bearer k", got)
		}
		fmt.Fprint(w, `{"data":[{"id":"gpt-5.5"},{"id":"gpt-5.4-mini"}]}`)
	}))
	defer srv.Close()

	t.Setenv("EVOLVE_OPENAI_API_KEY", "k")
	l := openaiLister{url: srv.URL, envKeys: CounterEnvKeys(ProviderOpenAI), client: srv.Client()}
	got, err := l.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(got) != 2 || got[0].ID != "gpt-5.5" || got[1].ID != "gpt-5.4-mini" {
		t.Errorf("got %v, want gpt-5.5, gpt-5.4-mini", got)
	}
}

// TestGoogleLister covers the "models/" prefix strip, the generateContent
// filter, and pageToken pagination in one pass.
func TestGoogleLister(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-goog-api-key"); got != "k" {
			t.Errorf("x-goog-api-key = %q, want k", got)
		}
		switch r.URL.Query().Get("pageToken") {
		case "":
			fmt.Fprint(w, `{"models":[
				{"name":"models/gemini-3.5-flash","displayName":"Gemini 3.5 Flash","supportedGenerationMethods":["generateContent"]},
				{"name":"models/embedding-001","displayName":"Embedding","supportedGenerationMethods":["embedContent"]}
			],"nextPageToken":"p2"}`)
		case "p2":
			fmt.Fprint(w, `{"models":[
				{"name":"models/gemini-3.1-pro","displayName":"Gemini 3.1 Pro","supportedGenerationMethods":["generateContent"]}
			]}`)
		default:
			t.Errorf("unexpected pageToken %q", r.URL.Query().Get("pageToken"))
		}
	}))
	defer srv.Close()

	t.Setenv("EVOLVE_GOOGLE_API_KEY", "k")
	l := googleLister{url: srv.URL, envKeys: CounterEnvKeys(ProviderGoogle), client: srv.Client()}
	got, err := l.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	want := []DiscoveredModel{
		{ID: "gemini-3.5-flash", Name: "Gemini 3.5 Flash"},
		{ID: "gemini-3.1-pro", Name: "Gemini 3.1 Pro"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d models, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("model[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestGetJSONErrorTail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"nope"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	var out any
	err := getJSON(context.Background(), srv.Client(), srv.URL, nil, &out)
	if err == nil {
		t.Fatal("getJSON: want error on 403")
	}
}
