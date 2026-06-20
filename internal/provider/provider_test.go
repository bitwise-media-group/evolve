// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// roundTripperFunc adapts a function to http.RoundTripper so tests can inject
// transport-level failures. Shared by the cross-provider counting tests below.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestCountTokensHTTPError and TestCountTokensWrapsTransportError exercise the
// shared HTTP plumbing (http.go's postJSON) rather than any one provider's
// distinct code, so they live here with the cross-provider helper instead of in
// a single provider's file.

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

func TestCountTokensWrapsTransportError(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) TokenCounter
		model string
	}{
		{
			name: "anthropic",
			setup: func(t *testing.T) TokenCounter {
				t.Setenv("ANTHROPIC_API_KEY", "a-key")
				a := NewAnthropic()
				a.CountURL = "http://example.invalid"
				a.Client = &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						return nil, net.ErrClosed
					}),
				}
				return a
			},
			model: "claude-fable-5",
		},
		{
			name: "google",
			setup: func(t *testing.T) TokenCounter {
				t.Setenv("GEMINI_API_KEY", "g-key")
				g := NewGoogle()
				g.CountURLBase = "http://example.invalid/models/"
				g.Client = &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						return nil, net.ErrClosed
					}),
				}
				return g
			},
			model: "gemini-3.5-flash",
		},
		{
			name: "openai",
			setup: func(t *testing.T) TokenCounter {
				t.Setenv("OPENAI_API_KEY", "o-key")
				o := NewOpenAI()
				o.CountURL = "http://example.invalid"
				o.Client = &http.Client{
					Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
						return nil, net.ErrClosed
					}),
				}
				return o
			},
			model: "gpt-5.5",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counter := tc.setup(t)
			if _, err := counter.CountTokens(context.Background(), tc.model, "x"); !errors.Is(err, net.ErrClosed) {
				t.Fatalf("errors.Is(err, net.ErrClosed) = false; err = %v", err)
			}
		})
	}
}
