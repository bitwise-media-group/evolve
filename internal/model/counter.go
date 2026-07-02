// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// TokenCounter counts input tokens through a vendor's official counting API.
// Counting is a vendor concern, not a harness one: the same model is priced by
// its vendor regardless of which CLI ran it, so a counter is keyed by provider
// id (see CounterFor), and it always counts against the vendor's own model id
// (Model.BareID), never a harness-specific id.
type TokenCounter interface {
	CountTokens(ctx context.Context, modelID, text string) (int, error)
}

// ErrNoCredential reports that none of a vendor's credential env vars is set,
// so its counting API cannot be called.
var ErrNoCredential = errors.New("no API key or OAuth token set")

// defaultClient serves the token-counting APIs; generous because counting large
// SKILL.md payloads can be slow.
var defaultClient = &http.Client{Timeout: 60 * time.Second}

// CounterFor returns the counting client for a provider id, or (nil, false) for
// a vendor with no counting API (Cursor). Cursor/Copilot/Antigravity harnesses
// report no usage either, so their models stay token-less end-to-end.
func CounterFor(providerID string) (TokenCounter, bool) {
	switch providerID {
	case ProviderAnthropic:
		return anthropicCounter{
			url: "https://api.anthropic.com/v1/messages/count_tokens",
			envKeys: []string{
				"EVOLVE_ANTHROPIC_API_KEY", "EVOLVE_CLAUDE_CODE_OAUTH_TOKEN",
				"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN",
			},
			client: defaultClient,
		}, true
	case ProviderOpenAI:
		return openaiCounter{
			url:     "https://api.openai.com/v1/responses/input_tokens",
			envKeys: []string{"EVOLVE_OPENAI_API_KEY", "OPENAI_API_KEY"},
			client:  defaultClient,
		}, true
	case ProviderGoogle:
		return googleCounter{
			urlBase: "https://generativelanguage.googleapis.com/v1beta/models/",
			envKeys: []string{"EVOLVE_GOOGLE_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY"},
			client:  defaultClient,
		}, true
	}
	return nil, false
}

// CounterEnvKeys returns the credential env vars a provider's counting API
// reads, in preference order, or nil for a vendor with no counting API.
func CounterEnvKeys(providerID string) []string {
	switch providerID {
	case ProviderAnthropic:
		return []string{
			"EVOLVE_ANTHROPIC_API_KEY", "EVOLVE_CLAUDE_CODE_OAUTH_TOKEN",
			"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN",
		}
	case ProviderOpenAI:
		return []string{"EVOLVE_OPENAI_API_KEY", "OPENAI_API_KEY"}
	case ProviderGoogle:
		return []string{"EVOLVE_GOOGLE_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY"}
	}
	return nil
}

// anthropicCounter calls POST /v1/messages/count_tokens with an API key
// (x-api-key) or an OAuth token (Authorization: Bearer + the oauth beta header).
type anthropicCounter struct {
	url     string
	envKeys []string
	client  *http.Client
}

func (a anthropicCounter) CountTokens(ctx context.Context, modelID, text string) (int, error) {
	headers := anthropicAuthHeaders(a.envKeys)
	if headers == nil {
		return 0, ErrNoCredential
	}
	body := map[string]any{
		"model":    modelID,
		"messages": []map[string]any{{"role": "user", "content": text}},
	}
	var resp struct {
		InputTokens *int `json:"input_tokens"`
	}
	if err := postJSON(ctx, a.client, a.url, headers, body, &resp); err != nil {
		return 0, fmt.Errorf("anthropic count tokens: %w", err)
	}
	if resp.InputTokens == nil {
		return 0, fmt.Errorf("count_tokens response missing input_tokens")
	}
	return *resp.InputTokens, nil
}

// anthropicAuthHeaders resolves the first set credential env var into request
// headers, or nil when none is set. Shared by the counting and listing clients.
func anthropicAuthHeaders(envKeys []string) map[string]string {
	for _, env := range envKeys {
		value := os.Getenv(env)
		if value == "" {
			continue
		}
		// Pick the header style by credential kind, not the literal var name:
		// any *_API_KEY is an API key sent via x-api-key; OAuth/auth tokens go on
		// Authorization: Bearer with the oauth beta header.
		if strings.HasSuffix(env, "_API_KEY") {
			return map[string]string{"x-api-key": value, "anthropic-version": "2023-06-01"}
		}
		return map[string]string{
			"authorization":     "Bearer " + value,
			"anthropic-version": "2023-06-01",
			"anthropic-beta":    "oauth-2025-04-20",
		}
	}
	return nil
}

// openaiCounter calls POST /v1/responses/input_tokens.
type openaiCounter struct {
	url     string
	envKeys []string
	client  *http.Client
}

func (o openaiCounter) CountTokens(ctx context.Context, modelID, text string) (int, error) {
	key := firstEnv(o.envKeys)
	if key == "" {
		return 0, ErrNoCredential
	}
	headers := map[string]string{"authorization": "Bearer " + key}
	body := map[string]any{"model": modelID, "input": text}
	var resp struct {
		InputTokens *int `json:"input_tokens"`
	}
	if err := postJSON(ctx, o.client, o.url, headers, body, &resp); err != nil {
		return 0, fmt.Errorf("openai count tokens: %w", err)
	}
	if resp.InputTokens == nil {
		return 0, fmt.Errorf("input_tokens response missing input_tokens")
	}
	return *resp.InputTokens, nil
}

// googleCounter calls POST /v1beta/models/{model}:countTokens.
type googleCounter struct {
	urlBase string
	envKeys []string
	client  *http.Client
}

func (g googleCounter) CountTokens(ctx context.Context, modelID, text string) (int, error) {
	key := firstEnv(g.envKeys)
	if key == "" {
		return 0, ErrNoCredential
	}
	headers := map[string]string{"x-goog-api-key": key}
	body := map[string]any{"contents": []map[string]any{{"parts": []map[string]any{{"text": text}}}}}
	var resp struct {
		TotalTokens *int `json:"totalTokens"`
	}
	if err := postJSON(ctx, g.client, g.urlBase+modelID+":countTokens", headers, body, &resp); err != nil {
		return 0, fmt.Errorf("google count tokens: %w", err)
	}
	if resp.TotalTokens == nil {
		return 0, fmt.Errorf("countTokens response missing totalTokens")
	}
	return *resp.TotalTokens, nil
}

func firstEnv(keys []string) string {
	for _, env := range keys {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	return ""
}

// postJSON posts body to url and decodes the JSON response into out, returning
// the status and a response tail on non-2xx.
func postJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		tail, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("HTTP %s: %s", resp.Status, bytes.TrimSpace(tail))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
