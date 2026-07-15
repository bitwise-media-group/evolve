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

// claudeDefaultAllowedTools is the Claude tool grammar evals run with when they
// do not specify their own (ported from the Python harness's DEFAULT_TOOLS).
const claudeDefaultAllowedTools = "Read Write Edit Glob Grep Skill Bash(terraform *) Bash(tflint *) Bash(mkdir *)"

// Claude drives the `claude` CLI (Claude Code).
type Claude struct {
	base
}

// NewClaude returns the builtin Claude Code harness.
func NewClaude() *Claude {
	return &Claude{base: base{
		id:   model.HarnessClaude,
		name: "Claude Code",
		clis: []string{"claude"},
		// Credentials the claude CLI itself authenticates with. Both an API-key
		// and an OAuth-token form are accepted.
		envKeys: []string{
			"EVOLVE_ANTHROPIC_API_KEY", "EVOLVE_CLAUDE_CODE_OAUTH_TOKEN",
			"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN",
		},
		skillDirs: []string{filepath.Join(".claude", "skills")},
	}}
}

// claudeSandboxOff disables Claude Code's own Bash-tool OS sandbox via an inline
// settings override. evolve confines the whole `claude` process in its own
// sandbox, and Claude's Bash sandbox uses macOS Seatbelt, which cannot nest — so
// without this every Bash command in the agent dies with "Operation not
// permitted". It is passed only when evolve's sandbox is active (HostSandboxed);
// with evolve unconfined, Claude keeps its own sandbox. A managed-settings.json
// that forces the sandbox on still wins, so those hosts must use --no-sandbox.
const claudeSandboxOff = `{"sandbox":{"enabled":false}}`

func (c *Claude) TriggerSpec(ws, query, cliModelID string, hostSandboxed bool) model.CommandSpec {
	argv := []string{
		"claude", "-p", query,
		"--model", cliModelID,
		"--output-format", "stream-json",
		"--verbose",
		"--max-turns", "2",
		"--allowedTools", "Skill Read",
	}
	if hostSandboxed {
		argv = append(argv, "--settings", claudeSandboxOff)
	}
	return model.CommandSpec{Argv: argv, Dir: ws}
}

// claudeContentBlock is one content block of a Claude message in stream-json
// output. A tool_use block carries the invoked tool's name and the raw JSON
// arguments (an MCP tool surfaces with name "mcp__<server>__<tool>").
type claudeContentBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// claudeUsage is the token/cost accounting Claude reports on its terminal
// result event. Cache reads and writes are kept on their own fields; see
// ParseEvalOutput for why they are not folded into input.
type claudeUsage struct {
	InputTokens              int  `json:"input_tokens"`
	CacheCreationInputTokens int  `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int  `json:"cache_read_input_tokens"`
	OutputTokens             *int `json:"output_tokens"`
}

// claudeEvent is one line of Claude Code's stream-json (--verbose) output.
// Assistant events carry message.content blocks (text and tool_use); the
// terminal type:"result" event carries the final answer, usage, cost, and the
// error envelope (is_error/subtype/errors). Each event populates only its own
// fields, so the unused ones stay zero on the others.
type claudeEvent struct {
	Type    string `json:"type"`
	Message struct {
		Content []claudeContentBlock `json:"content"`
	} `json:"message"`
	Result       string       `json:"result"`
	IsError      bool         `json:"is_error"`
	Subtype      string       `json:"subtype"`
	Errors       []string     `json:"errors"`
	Usage        *claudeUsage `json:"usage"`
	TotalCostUSD *float64     `json:"total_cost_usd"`
}

// scanEvents walks Claude Code's stream-json output once: it returns the
// terminal result event (found is false when the output carried none — e.g.
// plain text or a crash mid-stream) and every tool_use invocation in observed
// order. ParseEvalOutput, ParseToolCalls, and RuntimeError each project from it.
func scanEvents(stdout []byte) (result claudeEvent, found bool, tools []model.ToolCall) {
	for _, line := range bytes.Split(stdout, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev claudeEvent
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		if ev.Type == "result" {
			result, found = ev, true
		}
		for _, block := range ev.Message.Content {
			if block.Type == "tool_use" {
				tools = append(tools, model.ToolCall{Name: block.Name, Input: block.Input})
			}
		}
	}
	return result, found, tools
}

// ScanLine reports a hit when a Skill or Read tool_use in the stream-json event
// targets the skill.
func (c *Claude) ScanLine(line []byte, skill, _ string) (bool, string) {
	var event claudeEvent
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

func (c *Claude) EvalSpec(ws string, in model.EvalInput, cliModelID string) model.CommandSpec {
	maxTurns := in.MaxTurns
	if maxTurns == 0 {
		maxTurns = model.DefaultMaxTurns
	}
	tools := in.AllowedTools
	if tools == "" {
		tools = claudeDefaultAllowedTools
	}
	argv := []string{
		"claude", "-p", in.Prompt,
		"--model", cliModelID,
		"--output-format", "stream-json",
		"--verbose",
		"--max-turns", strconv.Itoa(maxTurns),
		"--allowedTools", tools,
	}
	if in.HostSandboxed {
		argv = append(argv, "--settings", claudeSandboxOff)
	}
	return model.CommandSpec{Argv: argv, Dir: ws}
}

// ParseEvalOutput reads the final answer and usage from the terminal result
// event of claude's stream-json output. Cache writes and reads are reported on
// their own fields rather than folded into input: a multi-turn cached session
// re-reads the same base context every turn, so lumping cache reads into
// "input" inflates it many-fold over the (cheaply cached) reality.
// total_cost_usd still reflects everything the session consumed. Output with no
// result event (plain text, crash) falls back to the raw stdout and nil usage.
func (c *Claude) ParseEvalOutput(stdout []byte) (string, *model.Usage) {
	result, found, _ := scanEvents(stdout)
	if !found {
		return string(stdout), nil
	}
	if result.Usage == nil {
		return result.Result, nil
	}
	in := result.Usage.InputTokens
	cacheRead := result.Usage.CacheReadInputTokens
	cacheCreation := result.Usage.CacheCreationInputTokens
	return result.Result, &model.Usage{
		InputTokens:         &in,
		CacheReadTokens:     &cacheRead,
		CacheCreationTokens: &cacheCreation,
		OutputTokens:        result.Usage.OutputTokens,
		CostUSD:             result.TotalCostUSD,
	}
}

// ParseToolCalls returns every tool_use invocation in claude's stream-json
// output, in observed order. MCP tools surface as mcp__<server>__<tool>. The
// ToolCallReporter contract is satisfied: a run with no tool calls yields nil
// here, which the engine normalizes to a non-nil empty slice (assertion fails),
// reserving nil for harnesses that cannot report tool calls at all.
func (c *Claude) ParseToolCalls(stdout []byte) []model.ToolCall {
	_, _, tools := scanEvents(stdout)
	return tools
}

// ReportsUsage reports that the claude CLI reports session usage and cost.
func (c *Claude) ReportsUsage() bool { return true }

// RuntimeError detects a claude CLI run that produced no usable answer (auth
// blocked, init crash, error envelope without output) so it can be reported
// distinctly from an eval that ran and failed its assertions. A run with any
// non-empty result is gradable — this deliberately includes max-turns/partial
// runs, which the CLI reports with is_error=true but a populated result.
func (c *Claude) RuntimeError(stdout []byte, exitCode int, timedOut bool) string {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return "empty CLI output"
	}
	result, found, _ := scanEvents(stdout)
	if !found {
		if exitCode != 0 {
			return "unparseable CLI output"
		}
		return "" // a clean exit with plain-text output is degenerate but gradable
	}
	if result.Result != "" {
		return "" // there is an answer to grade (success, or a partial/max-turns run)
	}
	if result.IsError {
		return claudeErrorReason(result.Subtype, result.Errors)
	}
	return "" // empty-result success: grade it (assertions may inspect the workspace)
}

// claudeErrorReason renders the claude error envelope into one diagnostic line.
// The claude CLI reports a failed run only on stdout: the subtype names the
// class (error_max_turns, error_during_execution) and the `errors` array carries
// the human-readable detail. Neither is ever written to stderr, so without
// lifting them here the run surfaces as a bare non-zero exit with no explanation.
func claudeErrorReason(subtype string, errs []string) string {
	reason := "claude run error"
	if subtype != "" {
		reason += " (" + subtype + ")"
	}
	cleaned := make([]string, 0, len(errs))
	for _, e := range errs {
		if e = strings.TrimSpace(e); e != "" {
			cleaned = append(cleaned, e)
		}
	}
	if len(cleaned) > 0 {
		reason += ": " + strings.Join(cleaned, "; ")
	}
	return reason
}
