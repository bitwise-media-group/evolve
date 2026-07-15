// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"os/exec"

	"github.com/bitwise-media-group/evolve/internal/model"
)

// Harness is the required surface every agent CLI implements. A harness drives
// a model — it does not own one; the model the run targets is supplied as a
// harness-specific CLI id (see model.Model.CLIModelID), so the same Harness can
// run many vendors' models.
type Harness interface {
	ID() string        // registry key, e.g. "claude"
	Name() string      // human name, e.g. "Claude Code"
	CLI() []string     // runner binary candidates, in preference order
	EnvKeys() []string // credential env vars, in preference order
	SkillDirs() []string
	// TriggerSpec builds the headless command for one trigger query. cliModelID
	// is the harness-specific model id (already mapped from the canonical model).
	// hostSandboxed: when set, the harness must disable the agent CLI's own OS
	// sandbox so it does not nest illegally inside evolve's.
	TriggerSpec(ws, query, cliModelID string, hostSandboxed bool) model.CommandSpec
	// ScanLine inspects one stdout line for activation of skill. A non-empty
	// note surfaces a harness-reported run error as a warning.
	ScanLine(line []byte, skill string) (hit bool, note string)
}

// EvalRunner is the optional capability of running behavioral evals. Harnesses
// implement it only when their CLI supports a gradable headless run; engines
// type-assert and degrade for those that do not (Gemini).
type EvalRunner interface {
	EvalSpec(ws string, c model.EvalInput, cliModelID string) model.CommandSpec
	// ParseEvalOutput extracts the final assistant text and measured usage from
	// the CLI's full stdout. usage is nil where unsupported.
	ParseEvalOutput(stdout []byte) (finalText string, usage *model.Usage)
	// ReportsUsage reports whether live sessions ever yield measured usage;
	// false (cursor/copilot/antigravity) exempts the measured fields from --new
	// completeness.
	ReportsUsage() bool
	// RuntimeError returns a short reason when the agent run produced no usable
	// output (auth blocked, crash, empty/error envelope), or "" when the output
	// is gradable. A benign non-zero exit (e.g. max-turns) that still produced a
	// result returns "" — it is graded, not errored.
	RuntimeError(stdout []byte, exitCode int, timedOut bool) string
}

// ToolCallReporter is the optional capability of extracting the tool calls an
// agent made from the CLI's structured run output. Harnesses implement it only
// when their output carries tool invocations (Claude, Codex); envelope/text
// harnesses (Cursor, Copilot, Antigravity) cannot, so a tool_call assertion
// against them is skipped. Gemini will gain it for free once its EvalRunner
// lands — its ScanLine already parses the tool name and parameters.
type ToolCallReporter interface {
	// ParseToolCalls returns every tool invocation in the CLI's full stdout, in
	// the order observed. A non-nil empty slice means the run reported zero
	// calls (a tool_call assertion then fails); nil is reserved for harnesses
	// that cannot report tool calls at all (the assertion is skipped).
	ParseToolCalls(stdout []byte) []model.ToolCall
}

// harnessOrder is the deterministic preference order used to pick a harness for
// a model when several eligible harnesses support it and the model's preferred
// harness is not eligible.
var harnessOrder = []string{
	model.HarnessClaude, model.HarnessCodex, model.HarnessGemini,
	model.HarnessCursor, model.HarnessCopilot, model.HarnessAntigravity,
	model.HarnessGrok,
}

// All returns the builtin harness set, in harnessOrder.
func All() []Harness {
	return []Harness{
		NewClaude(), NewCodex(), NewGemini(), NewCursor(), NewCopilot(), NewAntigravity(),
		NewGrok(),
	}
}

// ByID returns the builtin harness with the given id, if any.
func ByID(id string) (Harness, bool) {
	for _, h := range All() {
		if h.ID() == id {
			return h, true
		}
	}
	return nil, false
}

// Available finds the first of the harness's runner binaries on PATH.
func Available(h Harness) (path string, ok bool) {
	for _, name := range h.CLI() {
		if p, err := exec.LookPath(name); err == nil {
			return p, true
		}
	}
	return "", false
}

// base carries the descriptive fields shared by all harnesses.
type base struct {
	id        string
	name      string
	clis      []string
	envKeys   []string
	skillDirs []string
}

func (b base) ID() string        { return b.id }
func (b base) Name() string      { return b.name }
func (b base) CLI() []string     { return b.clis }
func (b base) EnvKeys() []string { return b.envKeys }
func (b base) SkillDirs() []string {
	return b.skillDirs
}
