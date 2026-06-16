// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
)

// Cursor drives the Cursor agent CLI (`agent`, historically `cursor-agent`).
//
// Cursor exposes no token-counting API and its CLI reports no usage or cost,
// so it implements neither TokenCounter nor returns usage from case runs:
// estimate and measured fields stay absent in results and render as n/a.
// Cursor runs other vendors' models, so its model ids (e.g. "sonnet-4.5")
// are namespaced by the provider in results keys. The builtin model list is
// a conservative default — `agent models` prints the live list, and
// providers.cursor.models in the .evolve config file overrides it.
type Cursor struct {
	base
}

// NewCursor returns the builtin Cursor provider.
func NewCursor() *Cursor {
	return &Cursor{
		base: base{
			name:      "cursor",
			display:   "Cursor",
			clis:      []string{"agent", "cursor-agent"},
			envKeys:   []string{"CURSOR_API_KEY"},
			skillDirs: []string{filepath.Join(".cursor", "skills")},
			models: []Model{
				{ID: "composer-1", Display: "Cursor Composer 1"},
				{ID: "sonnet-4.5", Display: "Cursor — Sonnet 4.5"},
			},
		},
	}
}

// TriggerSpec builds the headless invocation. --force allows tool calls
// without interactive approval; stream-json emits tool_call events that make
// skill activation observable.
func (c *Cursor) TriggerSpec(ws, query, model string) CommandSpec {
	return CommandSpec{
		Argv: []string{
			"agent", "-p", query,
			"--output-format", "stream-json",
			"--model", model,
			"--workspace", ws,
			"--force",
		},
		Dir: ws,
	}
}

// ScanLine reports a hit when a tool_call event (e.g. a readToolCall) touches
// the skill's SKILL.md. The match is a substring of the serialized event,
// scoped to tool_call lines so assistant prose mentioning the path does not
// count.
func (c *Cursor) ScanLine(line []byte, skill string) (bool, string) {
	var event struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &event) != nil || event.Type != "tool_call" {
		return false, ""
	}
	return strings.Contains(string(line), "skills/"+skill+"/SKILL.md"), ""
}

func (c *Cursor) EvalSpec(ws string, in EvalInput, model string) CommandSpec {
	// Cursor has no equivalents of --max-turns or --allowedTools; its
	// sandboxing is runner-level (--force in a throwaway workspace).
	return CommandSpec{
		Argv: []string{
			"agent", "-p", in.Prompt,
			"--output-format", "json",
			"--model", model,
			"--workspace", ws,
			"--force",
		},
		Dir: ws,
	}
}

// ReportsUsage is a value indicating whether or not Cursor CLI exposes no usage or cost in any output format.
func (c *Cursor) ReportsUsage() bool { return false }

// ParseEvalOutput reads the final JSON result object. Cursor reports no
// usage, so usage is always nil.
func (c *Cursor) ParseEvalOutput(stdout []byte) (string, *Usage) {
	var payload struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(stdout, &payload) != nil || payload.Result == "" {
		return string(stdout), nil
	}
	return payload.Result, nil
}

// RuntimeError detects a cursor run that produced no result object (auth
// blocked, crash) so it is reported distinctly from a failed eval. A run with
// a non-empty result is gradable.
func (c *Cursor) RuntimeError(stdout []byte, exitCode int, timedOut bool) string {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return "empty CLI output"
	}
	var payload struct {
		Result string `json:"result"`
	}
	if json.Unmarshal(stdout, &payload) == nil && payload.Result != "" {
		return "" // gradable answer
	}
	if exitCode != 0 {
		return "cursor produced no result"
	}
	return ""
}
