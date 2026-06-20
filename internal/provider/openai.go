// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// OpenAI drives the `codex` CLI and the OpenAI input-token counting API.
type OpenAI struct {
	base
	CountURL string
	Client   *http.Client
}

// NewOpenAI returns the builtin OpenAI provider.
func NewOpenAI() *OpenAI {
	return &OpenAI{
		base: base{
			name:      "openai",
			display:   "OpenAI",
			clis:      []string{"codex"},
			envKeys:   []string{"EVOLVE_OPENAI_API_KEY", "OPENAI_API_KEY"},
			skillDirs: []string{filepath.Join(".agents", "skills")},
			models: []Model{
				// Spark is a research-preview Codex model; OpenAI has not published API pricing.
				{ID: "gpt-5.3-codex-spark", Display: "GPT-5.3 Codex Spark"},
				{ID: "gpt-5.4-mini", Display: "GPT-5.4 Mini", InputUSD: usd(0.75), OutputUSD: usd(4.50)},
				{ID: "gpt-5.4", Display: "GPT-5.4", InputUSD: usd(2.50), OutputUSD: usd(15.00)},
				{ID: "gpt-5.5", Display: "GPT-5.5", InputUSD: usd(5.00), OutputUSD: usd(30.00)},
			},
		},
		CountURL: "https://api.openai.com/v1/responses/input_tokens",
		Client:   defaultClient,
	}
}

func (o *OpenAI) TriggerSpec(ws, query, model string) CommandSpec {
	return CommandSpec{
		Argv: []string{"codex", "exec", query, "--json", "--skip-git-repo-check", "-m", model},
		Dir:  ws,
	}
}

// ScanLine is best-effort: any event-stream line mentioning the skill's
// SKILL.md path counts as an activation.
func (o *OpenAI) ScanLine(line []byte, skill string) (bool, string) {
	return strings.Contains(string(line), "skills/"+skill+"/SKILL.md"), ""
}

func (o *OpenAI) EvalSpec(ws string, c EvalInput, model string) CommandSpec {
	return CommandSpec{
		Argv: []string{
			"codex", "exec", c.Prompt,
			"--json", "--skip-git-repo-check",
			"--sandbox", "workspace-write",
			"-m", model,
		},
		Dir: ws,
	}
}

// ParseEvalOutput concatenates agent messages from the codex event stream and
// captures the last turn's usage. Codex reports tokens but not cost; the
// engine prices the tokens from the model matrix.
func (o *OpenAI) ParseEvalOutput(stdout []byte) (string, *Usage) {
	var texts []string
	var usage *Usage
	for line := range strings.SplitSeq(string(stdout), "\n") {
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Usage *struct {
				InputTokens       *int `json:"input_tokens"`
				CachedInputTokens *int `json:"cached_input_tokens"`
				OutputTokens      *int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.Type == "item.completed" && event.Item.Type == "agent_message" {
			texts = append(texts, event.Item.Text)
		}
		if event.Type == "turn.completed" && event.Usage != nil {
			// Codex reports input_tokens as the whole prompt with
			// cached_input_tokens a subset of it. The Usage contract wants
			// fresh (uncached) input on InputTokens and cache hits reported
			// separately, so split the cached portion off rather than letting
			// re-read context inflate the headline input figure.
			u := &Usage{OutputTokens: event.Usage.OutputTokens}
			if in := event.Usage.InputTokens; in != nil {
				fresh := *in
				if cached := event.Usage.CachedInputTokens; cached != nil {
					read := *cached
					if read > fresh {
						read = fresh
					}
					fresh -= read
					u.CacheReadTokens = &read
				}
				u.InputTokens = &fresh
			}
			usage = u
		}
	}
	if len(texts) == 0 {
		return string(stdout), usage
	}
	return strings.Join(texts, "\n"), usage
}

// ReportsUsage is a value indicating whether or not codex reports token usage (cost is computed from pricing).
func (o *OpenAI) ReportsUsage() bool { return true }

// RuntimeError detects a codex run that produced no agent output (auth
// blocked, crash) so it is reported distinctly from a failed eval. A run that
// emitted any agent_message event is gradable, regardless of exit code.
func (o *OpenAI) RuntimeError(stdout []byte, exitCode int, timedOut bool) string {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return "empty CLI output"
	}
	for line := range strings.SplitSeq(string(stdout), "\n") {
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
			} `json:"item"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.Type == "item.completed" && event.Item.Type == "agent_message" {
			return "" // produced agent output — gradable
		}
	}
	if exitCode != 0 {
		return "codex produced no agent output"
	}
	return ""
}

// CountTokens calls POST /v1/responses/input_tokens.
func (o *OpenAI) CountTokens(ctx context.Context, modelID, text string) (int, error) {
	var key string
	for _, env := range o.envKeys {
		if v := os.Getenv(env); v != "" {
			key = v
			break
		}
	}
	if key == "" {
		return 0, ErrNoCredential
	}
	headers := map[string]string{"authorization": "Bearer " + key}
	body := map[string]any{"model": modelID, "input": text}
	var resp struct {
		InputTokens *int `json:"input_tokens"`
	}
	if err := postJSON(ctx, o.Client, o.CountURL, headers, body, &resp); err != nil {
		return 0, fmt.Errorf("openai count tokens: %w", err)
	}
	if resp.InputTokens == nil {
		return 0, fmt.Errorf("input_tokens response missing input_tokens")
	}
	return *resp.InputTokens, nil
}
