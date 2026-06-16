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
	"strconv"
	"strings"
)

// DefaultAllowedTools is the Claude tool grammar evals run with when they do
// not specify their own (ported from the Python harness's DEFAULT_TOOLS).
const DefaultAllowedTools = "Read Write Edit Glob Grep Skill Bash(terraform *) Bash(tflint *) Bash(mkdir *)"

// Anthropic drives the `claude` CLI and the Anthropic counting API.
type Anthropic struct {
	base
	CountURL string
	Client   *http.Client
}

// NewAnthropic returns the builtin Anthropic provider.
func NewAnthropic() *Anthropic {
	return &Anthropic{
		base: base{
			name:    "anthropic",
			display: "Anthropic",
			clis:    []string{"claude"},
			// EVOLVE_ANTHROPIC_API_KEY lets token counting use a dedicated key
			// independent of whatever credential the claude CLI itself uses, so
			// a blocked CLAUDE_CODE_OAUTH_TOKEN does not also break cost estimates.
			envKeys:   []string{"EVOLVE_ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN"},
			skillDirs: []string{filepath.Join(".claude", "skills")},
			models: []Model{
				{ID: "claude-haiku-4-5", Display: "Claude Haiku 4.5", InputUSD: usd(1.00), OutputUSD: usd(5.00)},
				{ID: "claude-sonnet-4-6", Display: "Claude Sonnet 4.6", InputUSD: usd(3.00), OutputUSD: usd(15.00)},
				{ID: "claude-opus-4-8", Display: "Claude Opus 4.8", InputUSD: usd(5.00), OutputUSD: usd(25.00)},
				{ID: "claude-fable-5", Display: "Claude Fable 5", InputUSD: usd(10.00), OutputUSD: usd(50.00)},
			},
		},
		CountURL: "https://api.anthropic.com/v1/messages/count_tokens",
		Client:   defaultClient,
	}
}

func (a *Anthropic) TriggerSpec(ws, query, model string) CommandSpec {
	return CommandSpec{
		Argv: []string{
			"claude", "-p", query,
			"--model", model,
			"--output-format", "stream-json",
			"--verbose",
			"--max-turns", "2",
			"--allowedTools", "Skill Read",
		},
		Dir: ws,
	}
}

// ScanLine reports a hit when a Skill or Read tool_use in the stream-json
// event targets the skill.
func (a *Anthropic) ScanLine(line []byte, skill string) (bool, string) {
	var event struct {
		Message struct {
			Content []struct {
				Type  string          `json:"type"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &event) != nil {
		return false, ""
	}
	for _, block := range event.Message.Content {
		if block.Type != "tool_use" {
			continue
		}
		payload := string(block.Input)
		if block.Name == "Skill" && strings.Contains(payload, skill) {
			return true, ""
		}
		if block.Name == "Read" && strings.Contains(payload, "skills/"+skill+"/SKILL.md") {
			return true, ""
		}
	}
	return false, ""
}

func (a *Anthropic) EvalSpec(ws string, c EvalInput, model string) CommandSpec {
	maxTurns := c.MaxTurns
	if maxTurns == 0 {
		maxTurns = 25
	}
	tools := c.AllowedTools
	if tools == "" {
		tools = DefaultAllowedTools
	}
	return CommandSpec{
		Argv: []string{
			"claude", "-p", c.Prompt,
			"--model", model,
			"--output-format", "json",
			"--max-turns", strconv.Itoa(maxTurns),
			"--allowedTools", tools,
		},
		Dir: ws,
	}
}

// ParseEvalOutput reads the claude JSON payload. Cache writes/reads are input
// tokens too; they are folded in so the figure reflects everything the
// session consumed (cost_usd already does).
func (a *Anthropic) ParseEvalOutput(stdout []byte) (string, *Usage) {
	var payload struct {
		Result string `json:"result"`
		Usage  *struct {
			InputTokens              int  `json:"input_tokens"`
			CacheCreationInputTokens int  `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int  `json:"cache_read_input_tokens"`
			OutputTokens             *int `json:"output_tokens"`
		} `json:"usage"`
		TotalCostUSD *float64 `json:"total_cost_usd"`
	}
	if json.Unmarshal(stdout, &payload) != nil {
		return string(stdout), nil
	}
	if payload.Usage == nil {
		return payload.Result, nil
	}
	in := payload.Usage.InputTokens + payload.Usage.CacheCreationInputTokens + payload.Usage.CacheReadInputTokens
	return payload.Result, &Usage{
		InputTokens:  &in,
		OutputTokens: payload.Usage.OutputTokens,
		CostUSD:      payload.TotalCostUSD,
	}
}

// ReportsUsage is a value indicating whether or not the claude CLI reports session usage and cost.
func (a *Anthropic) ReportsUsage() bool { return true }

// RuntimeError detects a claude CLI run that produced no usable answer (auth
// blocked, init crash, error envelope without output) so it can be reported
// distinctly from an eval that ran and failed its assertions. A run with any
// non-empty result is gradable — this deliberately includes max-turns/partial
// runs, which the CLI reports with is_error=true but a populated result.
func (a *Anthropic) RuntimeError(stdout []byte, exitCode int, timedOut bool) string {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return "empty CLI output"
	}
	var env struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
		Subtype string `json:"subtype"`
	}
	if json.Unmarshal(stdout, &env) != nil {
		if exitCode != 0 {
			return "unparseable CLI output"
		}
		return "" // a clean exit with plain-text output is degenerate but gradable
	}
	if env.Result != "" {
		return "" // there is an answer to grade (success, or a partial/max-turns run)
	}
	if env.IsError {
		if env.Subtype != "" {
			return "claude run error (" + env.Subtype + ")"
		}
		return "claude run error"
	}
	return "" // empty-result success: grade it (assertions may inspect the workspace)
}

// CountTokens calls POST /v1/messages/count_tokens with an API key
// (x-api-key) or an OAuth token (Authorization: Bearer + the oauth beta
// header).
func (a *Anthropic) CountTokens(ctx context.Context, modelID, text string) (int, error) {
	headers := a.authHeaders()
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
	if err := postJSON(ctx, a.Client, a.CountURL, headers, body, &resp); err != nil {
		return 0, err
	}
	if resp.InputTokens == nil {
		return 0, fmt.Errorf("count_tokens response missing input_tokens")
	}
	return *resp.InputTokens, nil
}

func (a *Anthropic) authHeaders() map[string]string {
	for _, env := range a.envKeys {
		value := os.Getenv(env)
		if value == "" {
			continue
		}
		// Pick the header style by credential kind, not the literal var name:
		// any *_API_KEY (ANTHROPIC_API_KEY, EVOLVE_ANTHROPIC_API_KEY) is an API
		// key sent via x-api-key; OAuth/auth tokens go on Authorization: Bearer
		// with the oauth beta header.
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
