// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package model

import (
	"encoding/json"
	"math"
	"runtime"
)

// Provider is a model vendor: the entity that owns and prices a family of
// models (Anthropic, OpenAI, Google, Cursor). It is distinct from a harness —
// the CLI that drives a model — because several harnesses can run one vendor's
// model (Claude Code and Copilot both run Claude Sonnet).
type Provider struct {
	ID   string `json:"id"`   // registry key, e.g. "anthropic"
	Name string `json:"name"` // human name, e.g. "Anthropic"
}

// Model is one canonical, vendor-owned model. ID is provider-qualified
// ("anthropic/claude-sonnet-4-6") and is the stable results-file key; harness
// never appears in it. Pricing is USD per 1M tokens (standard tier, cache-miss
// rates); nil = the vendor has not published pricing for the model.
//
// Supported maps each harness id that can run this model to the CLI-specific
// model-id string that harness's --model flag expects — this is where harness
// id divergence lives (Claude Code wants "claude-sonnet-4-6", Copilot wants
// "claude-sonnet-4.6"). Preferred is the harness chosen when several supported
// harnesses are eligible; it is always a key of Supported.
type Model struct {
	ID         string            `json:"id"`
	ProviderID string            `json:"provider_id"`
	Name       string            `json:"name"`
	InputUSD   *float64          `json:"input_per_mtok"`
	OutputUSD  *float64          `json:"output_per_mtok"`
	Supported  map[string]string `json:"supported"`
	Preferred  string            `json:"preferred"`
}

// CommandSpec describes a runner invocation. Neither models nor harnesses ever
// touch os/exec; the runner package executes specs, so tests can fake
// execution entirely.
type CommandSpec struct {
	Argv []string // Argv[0] is replaced with the resolved CLI path before exec
	Dir  string   // workspace the agent runs in
	Env  []string // extras appended to os.Environ()
}

// Usage is the harness-reported consumption of one live agent session. Fields
// are nil where the CLI does not report them. InputTokens is the fresh
// (uncached) input only; cache reads and writes are reported separately so a
// multi-turn session's cheap cache traffic does not inflate the headline input
// figure.
type Usage struct {
	InputTokens         *int // fresh (uncached) input only
	CacheReadTokens     *int
	CacheCreationTokens *int
	OutputTokens        *int
	CostUSD             *float64
}

// ToolCall is one tool invocation observed in a harness's structured run output.
// Name is the invoked tool name — an MCP tool surfaces as "mcp__<server>__<tool>"
// under Claude and as the bare function name elsewhere. Input is the raw JSON
// arguments exactly as the CLI emitted them, or nil when reported without args.
type ToolCall struct {
	Name  string
	Input json.RawMessage
}

// DefaultMaxTurns is the agent-turn ceiling for a behavioral eval when neither
// the case nor the run overrides it. Kept in this lowest-level package so the
// runner, the CLI flag, and the config docs share one source of truth.
const DefaultMaxTurns = 20

// EvalInput is the runner-relevant subset of a behavioral case.
type EvalInput struct {
	Prompt       string
	MaxTurns     int    // 0 = the harness default (DefaultMaxTurns)
	AllowedTools string // "" = the harness default tool set
	// HostSandboxed reports that evolve already confines this run in its own OS
	// sandbox. Harnesses whose agent CLI applies its own OS sandbox must then
	// disable it: macOS Seatbelt (and the Linux equivalents) cannot nest, so a
	// second sandbox layer aborts every shell command the agent runs. When
	// false, evolve runs unconfined and the agent's own sandbox is the sole
	// protection, so it is left enabled.
	HostSandboxed bool
}

// DefaultJobs is the default agent-run concurrency: half the CPUs, rounded up.
// Runs are network-bound, so this is a politeness cap, not a CPU one.
func DefaultJobs() int {
	return (runtime.NumCPU() + 1) / 2
}

// InputCostUSD estimates the input cost of tokens at the model's input rate, or
// nil when the count or pricing is unavailable.
func InputCostUSD(m Model, tokens *int) *float64 {
	if tokens == nil || m.InputUSD == nil {
		return nil
	}
	cost := round6(float64(*tokens) / 1e6 * *m.InputUSD)
	return &cost
}

// UsageCostUSD prices a measured usage at the model's rates, or nil when
// pricing is unavailable. This is only the fallback for a runner that reports
// no total_cost_usd; it prices every input-side field (fresh input, cache
// reads, and cache writes) at the input rate so the figure still reflects the
// whole session — the same total the lumped input figure used to carry.
func UsageCostUSD(m Model, u Usage) *float64 {
	if m.InputUSD == nil || m.OutputUSD == nil {
		return nil
	}
	var cost float64
	for _, in := range []*int{u.InputTokens, u.CacheReadTokens, u.CacheCreationTokens} {
		if in != nil {
			cost += float64(*in) / 1e6 * *m.InputUSD
		}
	}
	if u.OutputTokens != nil {
		cost += float64(*u.OutputTokens) / 1e6 * *m.OutputUSD
	}
	cost = round6(cost)
	return &cost
}

func round6(x float64) float64 {
	return math.Round(x*1e6) / 1e6
}

func usd(x float64) *float64 { return new(x) }
