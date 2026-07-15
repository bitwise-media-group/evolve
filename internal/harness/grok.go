// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/model"
)

// grokDefaultAllowedTools mirrors claudeDefaultAllowedTools as Claude-compatible
// --allow rules (Grok documents --allow as the Claude Code --allowedTools analog).
const grokDefaultAllowedTools = "Read Write Edit Glob Grep Skill Bash(terraform *) Bash(tflint *) Bash(mkdir *)"

// Grok drives the `grok` CLI (Grok Build TUI).
type Grok struct {
	base
}

// NewGrok returns the builtin Grok harness.
func NewGrok() *Grok {
	return &Grok{base: base{
		id:   model.HarnessGrok,
		name: "Grok",
		clis: []string{"grok"},
		// API-key auth for headless/CI. Session auth via `grok login` also works
		// without an env var (doctor will report "not set" in that case).
		envKeys:   []string{"EVOLVE_XAI_API_KEY", "XAI_API_KEY"},
		skillDirs: []string{filepath.Join(".grok", "skills")},
	}}
}

// appendAllowRules expands a Claude-style tool grammar into repeated --allow
// flags Grok understands (e.g. "Read Bash(git *)" → --allow Read --allow
// Bash(git *)). Tokens are space-separated outside parentheses so patterns like
// Bash(terraform *) stay one rule.
func appendAllowRules(argv []string, tools string) []string {
	for _, rule := range splitAllowRules(tools) {
		argv = append(argv, "--allow", rule)
	}
	return argv
}

// splitAllowRules splits a Claude-style allowed-tools string on whitespace
// outside parentheses.
func splitAllowRules(tools string) []string {
	var rules []string
	var b strings.Builder
	depth := 0
	flush := func() {
		if s := strings.TrimSpace(b.String()); s != "" {
			rules = append(rules, s)
		}
		b.Reset()
	}
	for _, r := range tools {
		switch {
		case r == '(':
			depth++
			b.WriteRune(r)
		case r == ')':
			if depth > 0 {
				depth--
			}
			b.WriteRune(r)
		case r == ' ' || r == '\t' || r == '\n':
			if depth == 0 {
				flush()
			} else {
				b.WriteRune(r)
			}
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return rules
}

// grokSandboxArg returns the --sandbox profile for this run. When evolve already
// confines the process (hostSandboxed), Grok's own Seatbelt/Landlock sandbox
// must be off — the two cannot nest. When evolve is unconfined, use workspace
// so Grok supplies write confinement analogous to Claude Code's Bash sandbox.
func grokSandboxArg(hostSandboxed bool) string {
	if hostSandboxed {
		return "off"
	}
	return "workspace"
}

func (g *Grok) TriggerSpec(ws, query, cliModelID string, hostSandboxed bool) model.CommandSpec {
	argv := []string{
		"grok", "-p", query,
		"--model", cliModelID,
		"--output-format", "streaming-json",
		"--max-turns", "2",
		"--disable-web-search",
		"--sandbox", grokSandboxArg(hostSandboxed),
	}
	argv = appendAllowRules(argv, "Skill Read")
	return model.CommandSpec{Argv: argv, Dir: ws}
}

func (g *Grok) EvalSpec(ws string, in model.EvalInput, cliModelID string) model.CommandSpec {
	maxTurns := in.MaxTurns
	if maxTurns == 0 {
		maxTurns = model.DefaultMaxTurns
	}
	tools := in.AllowedTools
	if tools == "" {
		tools = grokDefaultAllowedTools
	}
	argv := []string{
		"grok", "-p", in.Prompt,
		"--model", cliModelID,
		"--output-format", "json",
		"--max-turns", strconv.Itoa(maxTurns),
		"--disable-web-search",
		"--sandbox", grokSandboxArg(in.HostSandboxed),
	}
	argv = appendAllowRules(argv, tools)
	return model.CommandSpec{Argv: argv, Dir: ws}
}

// ScanLine is best-effort: headless streaming-json documents text/thought/end/
// error only (no tool events), so any line mentioning the skill's SKILL.md path
// counts as an activation (same posture as codex/copilot).
func (g *Grok) ScanLine(line []byte, skill string) (bool, string) {
	return strings.Contains(string(line), "skills/"+skill+"/SKILL.md"), ""
}

// grokResult is the single JSON object emitted by `grok -p --output-format json`
// (and the terminal `end` event of streaming-json, which carries the same spend
// fields). See Grok headless-mode docs for the field policy: input_tokens is
// uncached only; cache hits live on cache_read_input_tokens.
type grokResult struct {
	Type         string   `json:"type"` // "error" on failure envelope
	Message      string   `json:"message"`
	Text         string   `json:"text"`
	StopReason   string   `json:"stopReason"`
	TotalCostUSD *float64 `json:"total_cost_usd"`
	Usage        *struct {
		InputTokens          int  `json:"input_tokens"`
		CacheReadInputTokens int  `json:"cache_read_input_tokens"`
		OutputTokens         *int `json:"output_tokens"`
	} `json:"usage"`
}

// ParseEvalOutput reads the final answer and usage from grok's JSON object.
// Non-JSON stdout falls back to the raw text with nil usage.
func (g *Grok) ParseEvalOutput(stdout []byte) (string, *model.Usage) {
	result, ok := parseGrokResult(stdout)
	if !ok {
		return string(stdout), nil
	}
	if result.Usage == nil {
		return result.Text, nil
	}
	in := result.Usage.InputTokens
	cacheRead := result.Usage.CacheReadInputTokens
	return result.Text, &model.Usage{
		InputTokens:     &in,
		CacheReadTokens: &cacheRead,
		OutputTokens:    result.Usage.OutputTokens,
		CostUSD:         result.TotalCostUSD,
	}
}

// ReportsUsage reports that the grok CLI reports session usage and cost in its
// JSON output.
func (g *Grok) ReportsUsage() bool { return true }

// RuntimeError detects a grok run that produced no usable answer (auth blocked,
// error envelope, empty output) so it is reported distinctly from a failed eval.
func (g *Grok) RuntimeError(stdout []byte, exitCode int, timedOut bool) string {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return "empty CLI output"
	}
	result, ok := parseGrokResult(stdout)
	if !ok {
		if exitCode != 0 {
			return "unparseable CLI output"
		}
		return "" // clean exit with plain text is degenerate but gradable
	}
	if result.Type == "error" {
		msg := strings.TrimSpace(result.Message)
		if msg == "" {
			return "grok run error"
		}
		return "grok run error: " + msg
	}
	if result.Text != "" {
		return "" // there is an answer to grade
	}
	if exitCode != 0 {
		return "grok produced no answer"
	}
	return "" // empty-result success: grade it (assertions may inspect the workspace)
}

// parseGrokResult decodes a single JSON object from stdout, or the last
// parseable line of a streaming-json stream (preferring an end/error event).
func parseGrokResult(stdout []byte) (grokResult, bool) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return grokResult{}, false
	}
	// Prefer a single-object decode (eval --output-format json).
	var single grokResult
	if json.Unmarshal(trimmed, &single) == nil && (single.Text != "" || single.Type != "" || single.Usage != nil || single.Message != "") {
		return single, true
	}
	// Fall back to the last meaningful event in a streaming-json stream.
	var last grokResult
	found := false
	for _, line := range bytes.Split(stdout, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev grokResult
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		if ev.Type == "end" || ev.Type == "error" || ev.Text != "" || ev.Usage != nil {
			last, found = ev, true
		}
	}
	return last, found
}
