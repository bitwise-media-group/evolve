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

func TestCounterFor(t *testing.T) {
	for _, pid := range []string{ProviderAnthropic, ProviderOpenAI, ProviderGoogle, ProviderXAI} {
		if _, ok := CounterFor(pid); !ok {
			t.Errorf("CounterFor(%q) = not found, want a counter", pid)
		}
		if len(CounterEnvKeys(pid)) == 0 {
			t.Errorf("CounterEnvKeys(%q) empty", pid)
		}
	}
	// Cursor has no counting API; harness-only ids are not vendors.
	for _, pid := range []string{ProviderCursor, "copilot", "unknown"} {
		if _, ok := CounterFor(pid); ok {
			t.Errorf("CounterFor(%q) = found, want none", pid)
		}
		if CounterEnvKeys(pid) != nil {
			t.Errorf("CounterEnvKeys(%q) non-nil", pid)
		}
	}
}

func TestXAICounter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer k" {
			t.Errorf("authorization = %q, want Bearer k", got)
		}
		fmt.Fprint(w, `{"token_ids":[{"token_id":1},{"token_id":2},{"token_id":3}]}`)
	}))
	defer srv.Close()

	t.Setenv("EVOLVE_XAI_API_KEY", "k")
	c := xaiCounter{url: srv.URL, envKeys: CounterEnvKeys(ProviderXAI), client: srv.Client()}
	n, err := c.CountTokens(context.Background(), "grok-4.5", "hello")
	if err != nil {
		t.Fatalf("CountTokens: %v", err)
	}
	if n != 3 {
		t.Errorf("CountTokens = %d, want 3", n)
	}
}

func TestXAICounterNoCredential(t *testing.T) {
	for _, env := range CounterEnvKeys(ProviderXAI) {
		t.Setenv(env, "")
	}
	c := xaiCounter{url: "http://unused.invalid", envKeys: CounterEnvKeys(ProviderXAI)}
	if _, err := c.CountTokens(context.Background(), "grok-4.5", "hi"); err != ErrNoCredential {
		t.Errorf("err = %v, want ErrNoCredential", err)
	}
}
