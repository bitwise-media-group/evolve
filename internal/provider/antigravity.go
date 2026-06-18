// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import (
	"bytes"
	"path/filepath"
	"strings"
)

// Antigravity drives Google's Antigravity CLI (`agy -p`, the non-interactive
// "print" runner — not the Antigravity IDE or the `antigravity run` automation
// surface).
//
// As of agy 1.0.9 the CLI has no machine-readable output mode (no
// --output-format/--output json flag) and no token-counting API, so Antigravity
// implements neither TokenCounter nor returns usage from case runs: estimate and
// measured fields stay absent in results and render as n/a. Like Cursor it runs
// other vendors' models (Gemini, Claude, GPT-OSS) billed by quota/subscription,
// so its model ids carry no published per-token pricing and are namespaced by
// the provider in results keys. The builtin model list is a conservative
// default — `agy models` prints the live list, and providers.antigravity.models
// in the .evolve config file overrides it.
//
// Operational note: agy must be able to write its app-data under
// ~/.gemini/antigravity-cli (logs, project config, OAuth token); a probe under a
// filesystem sandbox that blocked those writes failed to start a conversation.
type Antigravity struct {
	base
}

// NewAntigravity returns the builtin Antigravity provider.
func NewAntigravity() *Antigravity {
	return &Antigravity{
		base: base{
			name:    "antigravity",
			display: "Antigravity",
			// The CLI binary is `agy` (verified against agy --help, v1.0.9). The
			// Antigravity IDE / `antigravity run` 2.0 surface is a different tool
			// and is intentionally not a candidate here.
			clis: []string{"agy"},
			// A live probe (agy 1.0.9) authenticated via OAuth/keyring login with
			// no API-key env var set — agy expects `agy` login, not a credential
			// var. These keys are a best-effort fallback in case agy honors them.
			// The EVOLVE_-prefixed entry mirrors the google/openai convention.
			// TODO(verify): does agy read any API-key env var at all? If not, the
			// doctor credential check has nothing to detect for this provider.
			envKeys:   []string{"EVOLVE_ANTIGRAVITY_API_KEY", "ANTIGRAVITY_API_KEY"},
			skillDirs: []string{filepath.Join(".antigravity", "skills")},
			// agy bills by quota/subscription, so the vendor models it runs carry no
			// published API pricing here (estimate/measured render as n/a). Pin live
			// ids via providers.antigravity.models.
			// `agy models` (probed, authenticated) lists these display labels:
			// Gemini 3.5 Flash (Low/Medium/High), Gemini 3.1 Pro (Low/High),
			// Claude Sonnet 4.6 (Thinking), Claude Opus 4.6 (Thinking),
			// GPT-OSS 120B (Medium). TODO(verify): the exact --model selector
			// string (vs. these labels) is unconfirmed; pin real ids once known.
			models: []Model{
				{ID: "gemini-3.1-pro", Display: "Antigravity — Gemini 3.1 Pro"},
				{ID: "gemini-3.5-flash", Display: "Antigravity — Gemini 3.5 Flash"},
				{ID: "claude-sonnet-4.6", Display: "Antigravity — Claude Sonnet 4.6"},
			},
		},
	}
}

// TriggerSpec builds the headless invocation. --dangerously-skip-permissions
// auto-approves tool calls so the run does not pause for confirmation; agy 1.0.9
// has no structured-output flag, so skill activation must be observed in the
// plain-text stdout (see ScanLine).
//
// The probe showed agy resolves the process cwd as its workspaceDir, so
// CommandSpec.Dir should suffice (no --add-dir needed).
func (a *Antigravity) TriggerSpec(ws, query, model string) CommandSpec {
	return CommandSpec{
		Argv: []string{
			"agy", "-p", query,
			"--model", model,
			"--dangerously-skip-permissions",
		},
		Dir: ws,
	}
}

// ScanLine is best-effort: agy emits no structured events, so any stdout line
// mentioning the skill's SKILL.md path counts as an activation.
//
// TODO(verify): confirm `agy -p` echoes tool/file-read activity (the SKILL.md
// path) to stdout. The probe could not answer this — its runs were blocked by
// the filesystem sandbox before any tool executed, and agy emits heavy
// diagnostic noise on stderr (not stdout). If print mode emits only the final
// answer, trigger detection via stdout under-detects; the deferred fallback is
// to scan the --log-file transcript instead.
func (a *Antigravity) ScanLine(line []byte, skill string) (bool, string) {
	return strings.Contains(string(line), "skills/"+skill+"/SKILL.md"), ""
}

// EvalSpec builds the eval invocation. agy has no --max-turns/--allowedTools
// analogs; tool gating is runner-level (--dangerously-skip-permissions in a
// throwaway workspace), so in.MaxTurns/in.AllowedTools are intentionally unused.
func (a *Antigravity) EvalSpec(ws string, in EvalInput, model string) CommandSpec {
	return CommandSpec{
		Argv: []string{
			"agy", "-p", in.Prompt,
			"--model", model,
			"--dangerously-skip-permissions",
		},
		Dir: ws,
	}
}

// ReportsUsage is a value indicating whether or not the agy CLI exposes any
// usage or cost; it never does.
func (a *Antigravity) ReportsUsage() bool { return false }

// ParseEvalOutput returns the agent's plain-text answer. agy reports no usage,
// so usage is always nil.
//
// TODO(verify): confirm a trivial prompt's stdout is just the answer; if agy
// prints banners/decoration, strip them here.
func (a *Antigravity) ParseEvalOutput(stdout []byte) (string, *Usage) {
	return strings.TrimSpace(string(stdout)), nil
}

// RuntimeError detects an agy run that produced no answer (auth blocked, crash)
// so it is reported distinctly from a failed eval. With no result envelope to
// inspect, any non-empty text output is treated as gradable.
func (a *Antigravity) RuntimeError(stdout []byte, exitCode int, timedOut bool) string {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return "empty CLI output"
	}
	return "" // a text answer exists — grade it
}
