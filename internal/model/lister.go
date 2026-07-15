// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// DiscoveredModel is one model a vendor's model-listing API advertises: the
// bare vendor id (Model.BareID namespace, never harness-specific) and the
// vendor's display name ("" when the API publishes none). Listing APIs expose
// no pricing, so discovery can identify a model but never price it.
type DiscoveredModel struct {
	ID   string
	Name string
}

// ModelLister enumerates the models a vendor currently serves. Like counting,
// listing is a vendor concern keyed by provider id (see ListerFor): the same
// registry entry stays valid regardless of which harness drives the model.
type ModelLister interface {
	ListModels(ctx context.Context) ([]DiscoveredModel, error)
}

// ListerFor returns the model-listing client for a provider id, or (nil, false)
// for a vendor with no listing API (Cursor). Credentials are the same env vars
// the vendor's counting API reads (CounterEnvKeys).
func ListerFor(providerID string) (ModelLister, bool) {
	switch providerID {
	case ProviderAnthropic:
		return anthropicLister{
			url:     "https://api.anthropic.com/v1/models",
			envKeys: CounterEnvKeys(ProviderAnthropic),
			client:  defaultClient,
		}, true
	case ProviderOpenAI:
		return openaiLister{
			url:     "https://api.openai.com/v1/models",
			envKeys: CounterEnvKeys(ProviderOpenAI),
			client:  defaultClient,
		}, true
	case ProviderGoogle:
		return googleLister{
			url:     "https://generativelanguage.googleapis.com/v1beta/models",
			envKeys: CounterEnvKeys(ProviderGoogle),
			client:  defaultClient,
		}, true
	case ProviderXAI:
		return xaiLister{
			url:     "https://api.x.ai/v1/models",
			envKeys: CounterEnvKeys(ProviderXAI),
			client:  defaultClient,
		}, true
	}
	return nil, false
}

// anthropicLister calls GET /v1/models, following the after_id cursor until
// has_more is false. Auth matches the counting API: x-api-key for API keys,
// Authorization: Bearer + the oauth beta header for OAuth tokens.
type anthropicLister struct {
	url     string
	envKeys []string
	client  *http.Client
}

func (a anthropicLister) ListModels(ctx context.Context) ([]DiscoveredModel, error) {
	headers := anthropicAuthHeaders(a.envKeys)
	if headers == nil {
		return nil, ErrNoCredential
	}
	var out []DiscoveredModel
	afterID := ""
	for {
		q := url.Values{"limit": {"100"}}
		if afterID != "" {
			q.Set("after_id", afterID)
		}
		var resp struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := getJSON(ctx, a.client, a.url+"?"+q.Encode(), headers, &resp); err != nil {
			return nil, fmt.Errorf("anthropic list models: %w", err)
		}
		for _, m := range resp.Data {
			out = append(out, DiscoveredModel{ID: m.ID, Name: m.DisplayName})
		}
		if !resp.HasMore || resp.LastID == "" {
			return out, nil
		}
		afterID = resp.LastID
	}
}

// openaiLister calls GET /v1/models. The endpoint returns the full catalog in
// one page and carries no capability metadata, so every id is reported as-is
// (embeddings, audio, and image models included — callers filter by eye).
type openaiLister struct {
	url     string
	envKeys []string
	client  *http.Client
}

func (o openaiLister) ListModels(ctx context.Context) ([]DiscoveredModel, error) {
	key := firstEnv(o.envKeys)
	if key == "" {
		return nil, ErrNoCredential
	}
	headers := map[string]string{"authorization": "Bearer " + key}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := getJSON(ctx, o.client, o.url, headers, &resp); err != nil {
		return nil, fmt.Errorf("openai list models: %w", err)
	}
	var out []DiscoveredModel
	for _, m := range resp.Data {
		out = append(out, DiscoveredModel{ID: m.ID})
	}
	return out, nil
}

// googleLister calls GET /v1beta/models, following nextPageToken. Google's
// catalog mixes generative and embedding models; only those supporting
// generateContent are agent-drivable, so the rest are dropped here. The API's
// "models/" name prefix is stripped to keep ids in the registry namespace.
type googleLister struct {
	url     string
	envKeys []string
	client  *http.Client
}

func (g googleLister) ListModels(ctx context.Context) ([]DiscoveredModel, error) {
	key := firstEnv(g.envKeys)
	if key == "" {
		return nil, ErrNoCredential
	}
	headers := map[string]string{"x-goog-api-key": key}
	var out []DiscoveredModel
	pageToken := ""
	for {
		q := url.Values{"pageSize": {"1000"}}
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		var resp struct {
			Models []struct {
				Name        string   `json:"name"`
				DisplayName string   `json:"displayName"`
				Methods     []string `json:"supportedGenerationMethods"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := getJSON(ctx, g.client, g.url+"?"+q.Encode(), headers, &resp); err != nil {
			return nil, fmt.Errorf("google list models: %w", err)
		}
		for _, m := range resp.Models {
			if !slices.Contains(m.Methods, "generateContent") {
				continue
			}
			out = append(out, DiscoveredModel{
				ID:   strings.TrimPrefix(m.Name, "models/"),
				Name: m.DisplayName,
			})
		}
		if resp.NextPageToken == "" {
			return out, nil
		}
		pageToken = resp.NextPageToken
	}
}

// xaiLister calls GET /v1/models (OpenAI-compatible catalog).
type xaiLister struct {
	url     string
	envKeys []string
	client  *http.Client
}

func (x xaiLister) ListModels(ctx context.Context) ([]DiscoveredModel, error) {
	key := firstEnv(x.envKeys)
	if key == "" {
		return nil, ErrNoCredential
	}
	headers := map[string]string{"authorization": "Bearer " + key}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := getJSON(ctx, x.client, x.url, headers, &resp); err != nil {
		return nil, fmt.Errorf("xai list models: %w", err)
	}
	var out []DiscoveredModel
	for _, m := range resp.Data {
		out = append(out, DiscoveredModel{ID: m.ID})
	}
	return out, nil
}

// getJSON fetches endpoint and decodes the JSON response into out, returning
// the status and a response tail on non-2xx.
func getJSON(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
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
		return fmt.Errorf("HTTP %s: %s", resp.Status, strings.TrimSpace(string(tail)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
