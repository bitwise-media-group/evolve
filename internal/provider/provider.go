// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

// Model is one entry in a provider's matrix. Pricing is USD per 1M tokens
// (standard tier, cache-miss rates); nil = the provider has not published
// pricing for the model.
type Model struct {
	ID        string   `json:"id"`
	Display   string   `json:"display"`
	InputUSD  *float64 `json:"input_per_mtok"`
	OutputUSD *float64 `json:"output_per_mtok"`
}

// CommandSpec describes a runner invocation. Providers never touch os/exec;
// the runner package executes specs, so tests can fake execution entirely.
type CommandSpec struct {
	Argv []string // Argv[0] is replaced with the resolved CLI path before exec
	Dir  string   // workspace the agent runs in
	Env  []string // extras appended to os.Environ()
}

// Usage is the harness-reported consumption of one live agent session.
// Fields are nil where the CLI does not report them.
type Usage struct {
	InputTokens  *int
	OutputTokens *int
	CostUSD      *float64
}

// EvalInput is the runner-relevant subset of a behavioral case.
type EvalInput struct {
	Prompt       string
	MaxTurns     int    // 0 = the provider default (25)
	AllowedTools string // "" = the provider default tool set
}

// Provider is the required surface every provider implements.
type Provider interface {
	Name() string      // registry key, e.g. "anthropic"
	Display() string   // human name, e.g. "Anthropic"
	Models() []Model   // effective model matrix
	CLI() []string     // runner binary candidates, in preference order
	EnvKeys() []string // credential env vars, in preference order
	SkillDirs() []string
	// TriggerSpec builds the headless command for one trigger query.
	TriggerSpec(ws, query, model string) CommandSpec
	// ScanLine inspects one stdout line for activation of skill. A non-empty
	// note surfaces a provider-reported run error as a warning.
	ScanLine(line []byte, skill string) (hit bool, note string)
}

// EvalRunner is the optional capability of running behavioral evals.
type EvalRunner interface {
	EvalSpec(ws string, c EvalInput, model string) CommandSpec
	// ParseEvalOutput extracts the final assistant text and measured usage
	// from the CLI's full stdout. usage is nil where unsupported.
	ParseEvalOutput(stdout []byte) (finalText string, usage *Usage)
	// ReportsUsage reports whether live sessions ever yield measured usage;
	// false (cursor) exempts the measured fields from --new completeness.
	ReportsUsage() bool
	// RuntimeError returns a short reason when the agent run produced no
	// usable output (auth blocked, crash, empty/error envelope), or "" when
	// the output is gradable. A benign non-zero exit (e.g. max-turns) that
	// still produced a result returns "" — it is graded, not errored.
	RuntimeError(stdout []byte, exitCode int, timedOut bool) string
}

// TokenCounter is the optional capability of counting input tokens through
// the provider's official counting API.
type TokenCounter interface {
	CountTokens(ctx context.Context, modelID, text string) (int, error)
}

// ErrNoCredential reports that none of the provider's credential env vars is
// set, so its counting API cannot be called.
var ErrNoCredential = errors.New("no API key or OAuth token set")

// defaultClient serves the token-counting APIs; generous because counting
// large SKILL.md payloads can be slow.
var defaultClient = &http.Client{Timeout: 60 * time.Second}

// base carries the descriptive fields shared by all providers.
type base struct {
	name      string
	display   string
	clis      []string
	envKeys   []string
	skillDirs []string
	models    []Model
}

func (b *base) Name() string        { return b.name }
func (b *base) Display() string     { return b.display }
func (b *base) Models() []Model     { return b.models }
func (b *base) CLI() []string       { return b.clis }
func (b *base) EnvKeys() []string   { return b.envKeys }
func (b *base) SkillDirs() []string { return b.skillDirs }
func (b *base) setModels(m []Model) { b.models = m }

type modelSetter interface{ setModels([]Model) }

// ResolveCLI finds the first of the provider's runner binaries on PATH.
func ResolveCLI(p Provider) (string, bool) {
	for _, name := range p.CLI() {
		if path, err := exec.LookPath(name); err == nil {
			return path, true
		}
	}
	return "", false
}

// DefaultJobs is the default agent-run concurrency: half the CPUs, rounded
// up. Runs are network-bound, so this is a politeness cap, not a CPU one.
func DefaultJobs() int {
	return (runtime.NumCPU() + 1) / 2
}

// InputCostUSD estimates the input cost of tokens at the model's input rate,
// or nil when the count or pricing is unavailable.
func InputCostUSD(m Model, tokens *int) *float64 {
	if tokens == nil || m.InputUSD == nil {
		return nil
	}
	cost := round6(float64(*tokens) / 1e6 * *m.InputUSD)
	return &cost
}

// UsageCostUSD prices a measured usage at the model's rates, or nil when
// pricing is unavailable.
func UsageCostUSD(m Model, u Usage) *float64 {
	if m.InputUSD == nil || m.OutputUSD == nil {
		return nil
	}
	var cost float64
	if u.InputTokens != nil {
		cost += float64(*u.InputTokens) / 1e6 * *m.InputUSD
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
