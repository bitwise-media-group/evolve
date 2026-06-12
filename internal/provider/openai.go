// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import (
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
			envKeys:   []string{"OPENAI_API_KEY"},
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

func (o *OpenAI) CaseSpec(ws string, c CaseInput, model string) CommandSpec {
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

// ParseCaseOutput concatenates agent messages from the codex event stream and
// captures the last turn's usage. Codex reports tokens but not cost; the
// engine prices the tokens from the model matrix.
func (o *OpenAI) ParseCaseOutput(stdout []byte) (string, *Usage) {
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
				InputTokens  *int `json:"input_tokens"`
				OutputTokens *int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.Type == "item.completed" && event.Item.Type == "agent_message" {
			texts = append(texts, event.Item.Text)
		}
		if event.Type == "turn.completed" && event.Usage != nil {
			usage = &Usage{InputTokens: event.Usage.InputTokens, OutputTokens: event.Usage.OutputTokens}
		}
	}
	if len(texts) == 0 {
		return string(stdout), usage
	}
	return strings.Join(texts, "\n"), usage
}

// ReportsUsage is a value indicating whether or not codex reports token usage (cost is computed from pricing).
func (o *OpenAI) ReportsUsage() bool { return true }

// CountTokens calls POST /v1/responses/input_tokens.
func (o *OpenAI) CountTokens(ctx context.Context, modelID, text string) (int, error) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return 0, ErrNoCredential
	}
	headers := map[string]string{"authorization": "Bearer " + key}
	body := map[string]any{"model": modelID, "input": text}
	var resp struct {
		InputTokens *int `json:"input_tokens"`
	}
	if err := postJSON(ctx, o.Client, o.CountURL, headers, body, &resp); err != nil {
		return 0, err
	}
	if resp.InputTokens == nil {
		return 0, fmt.Errorf("input_tokens response missing input_tokens")
	}
	return *resp.InputTokens, nil
}
