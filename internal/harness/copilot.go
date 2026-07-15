// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/model"
)

// Copilot drives GitHub's agentic Copilot CLI (`copilot -p`, the
// non-interactive runner — not the older `gh copilot` suggest/explain
// extension).
//
// The CLI has no machine-readable output mode (`-s` only strips decoration from
// the plain-text answer) and no token-counting API, so ReportsUsage is false and
// case runs yield no usage: estimate/measured fields stay absent and render n/a.
type Copilot struct {
	base
}

// NewCopilot returns the builtin Copilot harness.
func NewCopilot() *Copilot {
	return &Copilot{base: base{
		id:   model.HarnessCopilot,
		name: "GitHub Copilot",
		clis: []string{"copilot"},
		// Non-interactive auth precedence, highest first (GitHub Docs).
		envKeys:   []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"},
		skillDirs: []string{filepath.Join(".copilot", "skills")},
	}}
}

// TriggerSpec builds the headless invocation. --allow-all-tools runs tools
// without interactive approval and --no-ask-user keeps the run from pausing; -s
// is deliberately omitted so the CLI's tool/file activity stays visible to
// ScanLine (Copilot has no structured event stream to inspect otherwise).
// Copilot applies no OS sandbox of its own, so hostSandboxed is irrelevant.
func (c *Copilot) TriggerSpec(ws, query, cliModelID string, _ bool) model.CommandSpec {
	return model.CommandSpec{
		Argv: []string{
			"copilot", "-p", query,
			"--model", cliModelID,
			"--allow-all-tools",
			"--no-ask-user",
		},
		Dir: ws,
	}
}

// ScanLine is best-effort: Copilot emits no structured events, so any stdout
// line mentioning the skill's SKILL.md path counts as an activation.
func (c *Copilot) ScanLine(line []byte, skill, _ string) (bool, string) {
	return strings.Contains(string(line), "skills/"+skill+"/SKILL.md"), ""
}

func (c *Copilot) EvalSpec(ws string, in model.EvalInput, cliModelID string) model.CommandSpec {
	// Copilot has no --max-turns/--allowedTools analogs; tool gating is
	// runner-level (--allow-all-tools in a throwaway workspace). -s yields just
	// the agent's answer for grading.
	return model.CommandSpec{
		Argv: []string{
			"copilot", "-p", in.Prompt,
			"--model", cliModelID,
			"-s",
			"--allow-all-tools",
			"--no-ask-user",
		},
		Dir: ws,
	}
}

// ReportsUsage reports that the Copilot CLI exposes no usage or cost; it never
// does.
func (c *Copilot) ReportsUsage() bool { return false }

// ParseEvalOutput returns the agent's plain-text answer. Copilot reports no
// usage, so usage is always nil.
func (c *Copilot) ParseEvalOutput(stdout []byte) (string, *model.Usage) {
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
