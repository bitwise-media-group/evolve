// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/model"
)

// Antigravity drives Google's Antigravity CLI (`agy -p`, the non-interactive
// "print" runner — not the Antigravity IDE or the `antigravity run` automation
// surface).
//
// As of agy 1.0.9 the CLI has no machine-readable output mode and no
// token-counting API, so ReportsUsage is false and case runs yield no usage:
// estimate/measured fields stay absent and render n/a.
//
// Operational note: agy must be able to write its app-data under
// <home>/.gemini/antigravity-cli (logs, project config, OAuth token); a probe
// under a filesystem sandbox that blocked those writes failed to start a
// conversation. evolve therefore runs agy under an isolated fake $HOME inside
// the workspace (see agyEnv), which is always writable.
type Antigravity struct {
	base
}

// NewAntigravity returns the builtin Antigravity harness.
func NewAntigravity() *Antigravity {
	return &Antigravity{base: base{
		id:   model.HarnessAntigravity,
		name: "Antigravity",
		// The CLI binary is `agy` (verified against agy --help, v1.0.9). The
		// Antigravity IDE / `antigravity run` 2.0 surface is a different tool and
		// is intentionally not a candidate here.
		clis: []string{"agy"},
		// A live probe (agy 1.0.9) authenticated via OAuth/keyring login with no
		// API-key env var set — agy expects `agy` login, not a credential var.
		// These keys are a best-effort fallback in case agy honors them.
		// TODO(verify): does agy read any API-key env var at all?
		envKeys:   []string{"EVOLVE_ANTIGRAVITY_API_KEY", "ANTIGRAVITY_API_KEY"},
		skillDirs: []string{filepath.Join(".antigravity", "skills")},
	}}
}

// TriggerSpec builds the headless invocation. --dangerously-skip-permissions
// auto-approves tool calls so the run does not pause for confirmation; agy 1.0.9
// has no structured-output flag, so skill activation must be observed in the
// plain-text stdout (see ScanLine).
//
// The probe showed agy resolves the process cwd as its workspaceDir, so
// CommandSpec.Dir should suffice (no --add-dir needed). agy applies no OS
// sandbox of its own (--dangerously-skip-permissions already disables its
// gating), so hostSandboxed is irrelevant.
func (a *Antigravity) TriggerSpec(ws, query, cliModelID string, _ bool) model.CommandSpec {
	return model.CommandSpec{
		Argv: []string{
			"agy", "-p", query,
			"--model", cliModelID,
			"--dangerously-skip-permissions",
		},
		Dir: ws,
		Env: agyEnv(ws),
	}
}

// agyHomeRel is the workspace-relative fake $HOME evolve gives the agy CLI.
// agy hard-wires its state root to <home>/.gemini/antigravity-cli (a hidden
// --gemini_dir flag exists but its relocation semantics are unverified), so
// isolation overrides HOME itself: conversations, brain state, history, and
// logs all land here and die with the workspace. Project skills stay at
// .antigravity/skills (the skillDirs mount).
const agyHomeRel = ".evolve/agy-home"

// agyEnv returns the process env extras for an agy invocation in ws: a fake
// HOME with the operator's OAuth token bridged in.
func agyEnv(ws string) []string {
	home := isolatedDir(ws, agyHomeRel)
	ensureAgyHome(home)
	return []string{"HOME=" + home}
}

// ensureAgyHome creates <fake>/.gemini/antigravity-cli, links the operator's
// OAuth token (write-through so a mid-run refresh sticks), and bridges git
// config into the fake HOME. Best-effort per the isolate.go contract.
func ensureAgyHome(home string) {
	dir := filepath.Join(home, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	src := filepath.Join(operatorDir("", ".gemini"), "antigravity-cli")
	if sameFilePath(src, dir) {
		return
	}
	for _, name := range []string{"antigravity-oauth-token", "installation_id"} {
		linkFile(filepath.Join(src, name), filepath.Join(dir, name))
	}
	linkGitConfig(home)
}

// ScanLine is best-effort: agy emits no structured events, so any stdout line
// mentioning the skill's SKILL.md path counts as an activation.
//
// TODO(verify): confirm `agy -p` echoes tool/file-read activity (the SKILL.md
// path) to stdout. If print mode emits only the final answer, trigger detection
// via stdout under-detects; the deferred fallback is to scan the --log-file
// transcript instead.
func (a *Antigravity) ScanLine(line []byte, skill, _ string) (bool, string) {
	return strings.Contains(string(line), "skills/"+skill+"/SKILL.md"), ""
}

// EvalSpec builds the eval invocation. agy has no --max-turns analog
// (--dangerously-skip-permissions in a throwaway workspace is the shared
// bypass-permissions eval posture), so in.MaxTurns is intentionally unused.
func (a *Antigravity) EvalSpec(ws string, in model.EvalInput, cliModelID string) model.CommandSpec {
	return model.CommandSpec{
		Argv: []string{
			"agy", "-p", in.Prompt,
			"--model", cliModelID,
			"--dangerously-skip-permissions",
		},
		Dir: ws,
		Env: agyEnv(ws),
	}
}

// JudgeSpec reuses the eval posture: agy has no read-only or turn-cap analogs,
// so a judge session could in principle mutate the workspace it grades. Prefer
// a claude/codex-drivable judge model when evals mix llm with later file
// assertions.
func (a *Antigravity) JudgeSpec(ws string, in model.JudgeInput, cliModelID string) model.CommandSpec {
	return a.EvalSpec(ws, model.EvalInput{Prompt: in.Prompt, HostSandboxed: in.HostSandboxed}, cliModelID)
}

// ReportsUsage reports that the agy CLI exposes no usage or cost; it never does.
func (a *Antigravity) ReportsUsage() bool { return false }

// ParseEvalOutput returns the agent's plain-text answer. agy reports no usage,
// so usage is always nil.
//
// TODO(verify): confirm a trivial prompt's stdout is just the answer; if agy
// prints banners/decoration, strip them here.
func (a *Antigravity) ParseEvalOutput(stdout []byte) (string, *model.Usage) {
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
