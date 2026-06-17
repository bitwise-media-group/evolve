// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import (
	"bytes"
	"path/filepath"
	"strings"
)

// Copilot drives GitHub's agentic Copilot CLI (`copilot -p`, the
// non-interactive runner — not the older `gh copilot` suggest/explain
// extension).
//
// The CLI has no machine-readable output mode (`-s` only strips decoration
// from the plain-text answer) and no token-counting API, so Copilot implements
// neither TokenCounter nor returns usage from case runs: estimate and measured
// fields stay absent in results and render as n/a. Like Cursor it runs other
// vendors' models, so its model ids (e.g. "claude-sonnet-4.6") are namespaced
// by the provider in results keys. The builtin model list is a conservative
// default — providers.copilot.models in the .evolve config file overrides it.
type Copilot struct {
	base
}

// NewCopilot returns the builtin Copilot provider.
func NewCopilot() *Copilot {
	return &Copilot{
		base: base{
			name:    "copilot",
			display: "GitHub Copilot",
			clis:    []string{"copilot"},
			// Non-interactive auth precedence, highest first (GitHub Docs).
			envKeys:   []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"},
			skillDirs: []string{filepath.Join(".copilot", "skills")},
			// Copilot bills by subscription, not per-token, so the vendor models
			// it runs carry no published API pricing here (estimate/measured
			// figures render as n/a). Pin live ids via providers.copilot.models.
			models: []Model{
				{ID: "claude-sonnet-4.6", Display: "Copilot — Claude Sonnet 4.6"},
				{ID: "claude-haiku-4.5", Display: "Copilot — Claude Haiku 4.5"},
				{ID: "gpt-5.2", Display: "Copilot — GPT-5.2"},
				{ID: "gpt-5.3-codex", Display: "Copilot — GPT-5.3 Codex"},
			},
		},
	}
}

// TriggerSpec builds the headless invocation. --allow-all-tools runs tools
// without interactive approval and --no-ask-user keeps the run from pausing;
// -s is deliberately omitted so the CLI's tool/file activity stays visible to
// ScanLine (Copilot has no structured event stream to inspect otherwise).
func (c *Copilot) TriggerSpec(ws, query, model string) CommandSpec {
	return CommandSpec{
		Argv: []string{
			"copilot", "-p", query,
			"--model", model,
			"--allow-all-tools",
			"--no-ask-user",
		},
		Dir: ws,
	}
}

// ScanLine is best-effort: Copilot emits no structured events, so any stdout
// line mentioning the skill's SKILL.md path counts as an activation.
func (c *Copilot) ScanLine(line []byte, skill string) (bool, string) {
	return strings.Contains(string(line), "skills/"+skill+"/SKILL.md"), ""
}

func (c *Copilot) EvalSpec(ws string, in EvalInput, model string) CommandSpec {
	// Copilot has no --max-turns/--allowedTools analogs; tool gating is
	// runner-level (--allow-all-tools in a throwaway workspace). -s yields just
	// the agent's answer for grading.
	return CommandSpec{
		Argv: []string{
			"copilot", "-p", in.Prompt,
			"--model", model,
			"-s",
			"--allow-all-tools",
			"--no-ask-user",
		},
		Dir: ws,
	}
}

// ReportsUsage is a value indicating whether or not the Copilot CLI exposes any
// usage or cost; it never does.
func (c *Copilot) ReportsUsage() bool { return false }

// ParseEvalOutput returns the agent's plain-text answer. Copilot reports no
// usage, so usage is always nil.
func (c *Copilot) ParseEvalOutput(stdout []byte) (string, *Usage) {
	return strings.TrimSpace(string(stdout)), nil
}

// RuntimeError detects a Copilot run that produced no answer (auth blocked,
// crash) so it is reported distinctly from a failed eval. With no result
// envelope to inspect, any non-empty text output is treated as gradable.
func (c *Copilot) RuntimeError(stdout []byte, exitCode int, timedOut bool) string {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return "empty CLI output"
	}
	return "" // a text answer exists — grade it
}
