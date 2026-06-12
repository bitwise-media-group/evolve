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

// Google drives the `gemini` CLI and the Gemini countTokens API. It has no
// behavioral-case runner yet, so its models are token-counted only for cases.
type Google struct {
	base
	CountURLBase string // model id and :countTokens are appended
	Client       *http.Client
}

// NewGoogle returns the builtin Google provider.
func NewGoogle() *Google {
	return &Google{
		base: base{
			name:      "google",
			display:   "Google",
			clis:      []string{"gemini"},
			envKeys:   []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
			skillDirs: []string{filepath.Join(".gemini", "skills")},
			models: []Model{
				{ID: "gemini-3.1-flash-lite", Display: "Gemini 3.1 Flash-Lite", InputUSD: usd(0.25), OutputUSD: usd(1.50)},
				{ID: "gemini-3-flash-preview", Display: "Gemini 3 Flash (preview)", InputUSD: usd(0.50), OutputUSD: usd(3.00)},
				{ID: "gemini-3.5-flash", Display: "Gemini 3.5 Flash", InputUSD: usd(1.50), OutputUSD: usd(9.00)},
				// <=200K-token tier; long-context requests price higher.
				{ID: "gemini-3.1-pro-preview", Display: "Gemini 3.1 Pro (preview)", InputUSD: usd(2.00), OutputUSD: usd(12.00)},
			},
		},
		CountURLBase: "https://generativelanguage.googleapis.com/v1beta/models/",
		Client:       defaultClient,
	}
}

// TriggerSpec builds the gemini invocation. Only --output-format stream-json
// emits tool_use events; --skip-trust keeps headless runs alive when the
// folder-trust feature is enabled (temp workspaces are never trusted).
func (g *Google) TriggerSpec(ws, query, model string) CommandSpec {
	return CommandSpec{
		Argv: []string{"gemini", "-p", query, "-m", model, "--output-format", "stream-json", "--skip-trust"},
		Dir:  ws,
	}
}

// ScanLine reports a hit when an activate_skill tool_use names the skill (a
// read of the SKILL.md path counts as a fallback). A result/error event
// becomes a warning note and counts as no-trigger.
func (g *Google) ScanLine(line []byte, skill string) (bool, string) {
	var event struct {
		Type       string          `json:"type"`
		Status     string          `json:"status"`
		ToolName   string          `json:"tool_name"`
		Parameters json.RawMessage `json:"parameters"`
		Error      struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(line, &event) != nil {
		return false, ""
	}
	if event.Type == "result" && event.Status == "error" {
		message := event.Error.Message
		if len(message) > 200 {
			message = message[:200]
		}
		return false, "gemini run errored; counted as no-trigger: " + message
	}
	if event.Type != "tool_use" {
		return false, ""
	}
	payload := string(event.Parameters)
	if event.ToolName == "activate_skill" && strings.Contains(payload, skill) {
		return true, ""
	}
	return strings.Contains(payload, "skills/"+skill+"/SKILL.md"), ""
}

// CountTokens calls POST /v1beta/models/{model}:countTokens.
func (g *Google) CountTokens(ctx context.Context, modelID, text string) (int, error) {
	var key string
	for _, env := range g.envKeys {
		if v := os.Getenv(env); v != "" {
			key = v
			break
		}
	}
	if key == "" {
		return 0, ErrNoCredential
	}
	headers := map[string]string{"x-goog-api-key": key}
	body := map[string]any{"contents": []map[string]any{{"parts": []map[string]any{{"text": text}}}}}
	var resp struct {
		TotalTokens *int `json:"totalTokens"`
	}
	if err := postJSON(ctx, g.Client, g.CountURLBase+modelID+":countTokens", headers, body, &resp); err != nil {
		return 0, err
	}
	if resp.TotalTokens == nil {
		return 0, fmt.Errorf("countTokens response missing totalTokens")
	}
	return *resp.TotalTokens, nil
}
