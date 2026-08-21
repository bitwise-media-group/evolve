// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bitwise-media-group/evolve/internal/model"
)

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

// claudeConfigRel is the workspace-relative CLAUDE_CONFIG_DIR evolve gives the
// claude CLI. Sessions, project history, and auto-memory live here so runs do
// not touch the operator's real ~/.claude; the tree dies with the workspace.
// Project skills stay at .claude/skills (the skillDirs mount).
const claudeConfigRel = ".evolve/claude-home"

// claudeEnv returns the process env extras that point a claude invocation in
// ws at a throwaway workspace-rooted config dir.
func claudeEnv(ws string) []string {
	dir := isolatedDir(ws, claudeConfigRel)
	ensureClaudeConfig(dir)
	return []string{
		"CLAUDE_CONFIG_DIR=" + dir,
		"DISABLE_AUTOUPDATER=1",
	}
}

// ensureClaudeConfig creates the isolated config dir, seeds the state file,
// and links the operator's OAuth credentials. macOS stores those in the
// Keychain (global, no bridging needed); Linux keeps .credentials.json beside
// the config, so the link is what carries auth there. Best-effort per the
// isolate.go contract.
func ensureClaudeConfig(dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	seedClaudeState(dir)
	opDir := operatorDir("CLAUDE_CONFIG_DIR", ".claude")
	dst := filepath.Join(dir, ".credentials.json")
	linkFile(filepath.Join(opDir, ".credentials.json"), dst)
	bridgeClaudeKeychain(opDir, dst)
}

// claudeKeychainService is the macOS Keychain service name the claude CLI uses
// for the OAuth credentials of a given config dir. The CLI namespaces the
// entry per config dir — "Claude Code-credentials-" + the first 8 hex chars of
// sha256(dir) — so a claude run pointed at an isolated CLAUDE_CONFIG_DIR can
// never see the operator's entry. Observed against claude 2.1.220 by shimming
// `security` and diffing the find-generic-password service across config dirs.
func claudeKeychainService(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:4])
}

// bridgeClaudeKeychain (macOS) copies the operator's Keychain-held OAuth
// payload into the isolated config dir's .credentials.json — the CLI falls
// back to that file when its per-config-dir Keychain entry is missing (see
// claudeKeychainService), which is exactly the isolated case. The legacy
// unsuffixed service name covers installs that logged in before the CLI
// namespaced its entries. Skipped when a credential env var the CLI itself
// reads already authenticates the run (the EVOLVE_-prefixed variables are
// token-counting credentials and deliberately never reach the CLI), when the
// file exists (bridged from an operator .credentials.json), or off darwin;
// best-effort like the rest of isolate.go.
//
// This is the one exec outside internal/runner: the payload lives in the
// Keychain, not in a file, and /usr/bin/security is the claude CLI's own
// storage mechanism, so reading it back the same way is the only bridge
// available. Harness specs stay pure — this is setup, not agent execution.
func bridgeClaudeKeychain(opDir, dst string) {
	if runtime.GOOS != "darwin" || opDir == "" {
		return
	}
	for _, k := range []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN"} {
		if os.Getenv(k) != "" {
			return
		}
	}
	if _, err := os.Lstat(dst); err == nil {
		return
	}
	u, err := user.Current()
	if err != nil || u.Username == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, service := range []string{claudeKeychainService(opDir), "Claude Code-credentials"} {
		out, err := exec.CommandContext(ctx, "/usr/bin/security",
			"find-generic-password", "-a", u.Username, "-w", "-s", service).Output()
		if err != nil || len(bytes.TrimSpace(out)) == 0 {
			continue
		}
		_ = os.WriteFile(dst, out, 0o600)
		return
	}
}

// seedClaudeState writes the isolated .claude.json (with CLAUDE_CONFIG_DIR
// set, the state file lives inside the config dir) with onboarding marked done
// so headless -p runs never stall on first-run prompts. Nothing else carries
// over: logged-in state is purely a matter of reachable credentials (env var,
// .credentials.json, or the Keychain bridge), and the operator's session
// history, project state, and caches deliberately stay behind.
func seedClaudeState(dir string) {
	state := filepath.Join(dir, ".claude.json")
	if _, err := os.Lstat(state); err != nil {
		_ = os.WriteFile(state, []byte(`{"hasCompletedOnboarding":true}`+"\n"), 0o600)
	}
}

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
	return model.CommandSpec{Argv: argv, Dir: ws, Env: claudeEnv(ws)}
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

// claudeRateLimit is the rate_limit_info payload of a rate_limit_event stream
// line. Observed shape (claude 2.x): {"type":"rate_limit_event",
// "rate_limit_info":{"status":"allowed","resetsAt":<epoch>,
// "rateLimitType":"five_hour","overageStatus":"rejected"}}. Status flips to
// "rejected" once the window is spent and overage is declined.
type claudeRateLimit struct {
	Status        string `json:"status"`
	RateLimitType string `json:"rateLimitType"`
	ResetsAt      int64  `json:"resetsAt"`
}

// claudeEvent is one line of Claude Code's stream-json (--verbose) output.
// Assistant events carry message.content blocks (text and tool_use); the
// terminal type:"result" event carries the final answer, usage, cost, and the
// error envelope (is_error/subtype/errors); a rate_limit_event carries the
// account's rate-limit state. Each event populates only its own fields, so the
// unused ones stay zero on the others.
type claudeEvent struct {
	Type    string `json:"type"`
	Message struct {
		Content []claudeContentBlock `json:"content"`
	} `json:"message"`
	Result        string           `json:"result"`
	IsError       bool             `json:"is_error"`
	Subtype       string           `json:"subtype"`
	Errors        []string         `json:"errors"`
	Usage         *claudeUsage     `json:"usage"`
	TotalCostUSD  *float64         `json:"total_cost_usd"`
	RateLimitInfo *claudeRateLimit `json:"rate_limit_info"`
}

// scanEvents walks Claude Code's stream-json output once: it returns the
// terminal result event (found is false when the output carried none — e.g.
// plain text or a crash mid-stream), every tool_use invocation in observed
// order, and the last rate_limit_event seen (the status can transition
// mid-session, so last is authoritative; nil when none appeared).
// ParseEvalOutput, ParseToolCalls, and RuntimeError each project from it.
func scanEvents(stdout []byte) (result claudeEvent, found bool, tools []model.ToolCall, rateLimit *claudeRateLimit) {
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
		if ev.Type == "rate_limit_event" && ev.RateLimitInfo != nil {
			rateLimit = ev.RateLimitInfo
		}
		for _, block := range ev.Message.Content {
			if block.Type == "tool_use" {
				tools = append(tools, model.ToolCall{Name: block.Name, Input: block.Input})
			}
		}
	}
	return result, found, tools, rateLimit
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

// EvalSpec runs claude with permissions bypassed: evals grade what the agent
// builds, not what a tool allowlist happens to permit, and confinement comes
// from the sandbox (evolve's when HostSandboxed, Claude Code's own Bash
// sandbox otherwise) rather than from permission prompts.
func (c *Claude) EvalSpec(ws string, in model.EvalInput, cliModelID string) model.CommandSpec {
	maxTurns := in.MaxTurns
	if maxTurns == 0 {
		maxTurns = model.DefaultMaxTurns
	}
	argv := []string{
		"claude", "-p", in.Prompt,
		"--model", cliModelID,
		"--output-format", "stream-json",
		"--verbose",
		"--max-turns", strconv.Itoa(maxTurns),
		"--permission-mode", "bypassPermissions",
	}
	if in.HostSandboxed {
		argv = append(argv, "--settings", claudeSandboxOff)
	}
	return model.CommandSpec{Argv: argv, Dir: ws, Env: claudeEnv(ws)}
}

// ParseEvalOutput reads the final answer and usage from the terminal result
// event of claude's stream-json output. Cache writes and reads are reported on
// their own fields rather than folded into input: a multi-turn cached session
// re-reads the same base context every turn, so lumping cache reads into
// "input" inflates it many-fold over the (cheaply cached) reality.
// total_cost_usd still reflects everything the session consumed. Output with no
// result event (plain text, crash) falls back to the raw stdout and nil usage.
func (c *Claude) ParseEvalOutput(stdout []byte) (string, *model.Usage) {
	result, found, _, _ := scanEvents(stdout)
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
	_, _, tools, _ := scanEvents(stdout)
	return tools
}

// ReportsUsage reports that the claude CLI reports session usage and cost.
func (c *Claude) ReportsUsage() bool { return true }

// RuntimeError detects a claude CLI run that produced no usable answer (auth
// blocked, init crash, error envelope without output) so it can be reported
// distinctly from an eval that ran and failed its assertions. A run with any
// non-empty result is gradable — this deliberately includes max-turns/partial
// runs, which the CLI reports with is_error=true but a populated result — with
// one carve-out: a usage-limit rejection. The CLI reports that as a
// success-shaped result (exit 0, the limit banner as the result text, zero
// output tokens), so without the carve-out rate-limited runs would be silently
// graded into all-fail rows.
func (c *Claude) RuntimeError(stdout []byte, exitCode int, timedOut bool) string {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return "empty CLI output"
	}
	result, found, _, rateLimit := scanEvents(stdout)
	if !found {
		if exitCode != 0 {
			return "unparseable CLI output"
		}
		return "" // a clean exit with plain-text output is degenerate but gradable
	}
	if reason := claudeUsageLimitReason(result, rateLimit); reason != "" {
		return reason
	}
	if result.Result != "" {
		return "" // there is an answer to grade (success, or a partial/max-turns run)
	}
	if result.IsError {
		return claudeErrorReason(result.Subtype, result.Errors)
	}
	return "" // empty-result success: grade it (assertions may inspect the workspace)
}

// claudeUsageLimitRE matches the usage-limit banner the claude CLI returns as
// its result text once the account's window is spent (exact phrasing varies by
// CLI version/path, e.g. "Claude AI usage limit reached|<epoch>").
var claudeUsageLimitRE = regexp.MustCompile(`(?i)usage limit reached|limit reached.*resets`)

// claudeUsageLimitReason classifies a run rejected by the account's usage
// limit: the last rate-limit event reports status "rejected" and the result
// carries zero output tokens, or the result text itself is a usage-limit
// banner (CLI versions/paths that emit the banner without a rejection event).
// A session that hit the limit mid-run but produced real output (non-zero
// output tokens, non-limit result text) stays gradable — only the observed
// 0-token rejection shape is reclassified.
func claudeUsageLimitReason(result claudeEvent, rateLimit *claudeRateLimit) string {
	limitText := claudeUsageLimitRE.MatchString(result.Result)
	zeroOutput := result.Usage == nil || result.Usage.OutputTokens == nil || *result.Usage.OutputTokens == 0
	rejected := rateLimit != nil && rateLimit.Status == "rejected"
	if !limitText && (!rejected || !zeroOutput) {
		return ""
	}
	if rateLimit != nil {
		return fmt.Sprintf("usage limit reached (%s), resets %s", rateLimit.RateLimitType,
			time.Unix(rateLimit.ResetsAt, 0).UTC().Format(time.RFC3339))
	}
	return "usage limit reached: " + strings.TrimSpace(result.Result)
}

// ListOfferedModels asks the installed claude CLI which models the operator's
// account is offered. `claude -p "/model"` is handled client-side (no API
// call) and prints the model-picker alias list — "Available: sonnet, opus,
// haiku, fable, …" — which tracks the account's plan and gated extras. Each
// bare alias is then resolved to its display name ("Sonnet 5") with a second
// client-side invocation, because aliases name families while the picker
// offers specific models: "sonnet" resolving to Sonnet 5 must not read as
// Sonnet 4.6 being offered. Bracketed variants (sonnet[1m]) duplicate a base
// alias's model and are skipped. Probes run against the operator's real
// config on purpose — an isolated CLAUDE_CONFIG_DIR would hide the account's
// gated model options.
func (c *Claude) ListOfferedModels(ctx context.Context, probe ProbeExec) ([]string, error) {
	out, err := probe(ctx, claudeProbeSpec(""), nil)
	if err != nil {
		return nil, err
	}
	aliases := parseClaudeModelAliases(out)
	if len(aliases) == 0 {
		return nil, nil
	}

	names := make([]string, len(aliases))
	var wg sync.WaitGroup
	for i, alias := range aliases {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := probe(ctx, claudeProbeSpec(alias), nil)
			if err != nil {
				return
			}
			names[i] = parseClaudeCurrentModel(out)
		}()
	}
	wg.Wait()

	var offered []string
	seen := map[string]bool{}
	for _, n := range names {
		if n != "" && !seen[n] {
			seen[n] = true
			offered = append(offered, n)
		}
	}
	return offered, nil
}

// claudeProbeSpec builds the client-side "/model" probe invocation, optionally
// pinning an alias to resolve. DISABLE_AUTOUPDATER keeps the probe from
// kicking off an update check; everything else uses the operator's config.
func claudeProbeSpec(alias string) model.CommandSpec {
	argv := []string{"claude", "-p", "/model"}
	if alias != "" {
		argv = append(argv, "--model", alias)
	}
	return model.CommandSpec{Argv: argv, Env: []string{"DISABLE_AUTOUPDATER=1"}}
}

// claudeMetaAliases are the "/model" aliases that name a selection mode, not a
// model: they resolve to a model some concrete alias already names (best,
// default) or to a mode description rather than a model name (opusplan —
// "Opus in plan mode, else Sonnet"). Resolving them adds nothing but junk
// tokens, so they are skipped.
var claudeMetaAliases = map[string]bool{"best": true, "default": true, "opusplan": true}

// parseClaudeModelAliases extracts the bare aliases from the "/model" usage
// line: `Usage: /model <name>. Available: sonnet, opus, …, or a full model
// ID.` Tokens with brackets (context-window variants of a base alias), the
// meta aliases, and the trailing "or a full model ID" prose are dropped.
func parseClaudeModelAliases(out []byte) []string {
	_, rest, found := strings.Cut(string(out), "Available:")
	if !found {
		return nil
	}
	if line, _, ok := strings.Cut(rest, "\n"); ok {
		rest = line
	}
	var aliases []string
	for tok := range strings.SplitSeq(rest, ",") {
		tok = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(tok), "."))
		if tok == "" || strings.ContainsAny(tok, "[] ") || claudeMetaAliases[tok] {
			continue
		}
		aliases = append(aliases, tok)
	}
	return aliases
}

// parseClaudeCurrentModel extracts the display name from a `Current model:
// Sonnet 5 (effort: high)` line, stripping any trailing parenthetical.
func parseClaudeCurrentModel(out []byte) string {
	_, rest, found := strings.Cut(string(out), "Current model:")
	if !found {
		return ""
	}
	if line, _, ok := strings.Cut(rest, "\n"); ok {
		rest = line
	}
	if name, _, ok := strings.Cut(rest, "("); ok {
		rest = name
	}
	return strings.TrimSpace(rest)
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
