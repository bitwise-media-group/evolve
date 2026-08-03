// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/model"
)

// HarnessJudge implements grade.Judge over a resolved judge selection: each
// verdict is one short session of the judge harness's CLI in the eval
// workspace, built by the harness's JudgeSpec and parsed by its
// ParseEvalOutput. Stateless per call, so safe under the sweep's eval
// concurrency.
type HarnessJudge struct {
	sel           harness.Selection
	eval          harness.EvalRunner
	cli           string
	runner        Runner
	hostSandboxed bool
}

// NewHarnessJudge binds a resolved judge selection to an executor. It errors
// when the harness lacks eval support (Gemini, until its EvalRunner lands) or
// its CLI is not on PATH — resolution failures surface at command start, never
// per-assertion.
func NewHarnessJudge(sel harness.Selection, r Runner, hostSandboxed bool) (*HarnessJudge, error) {
	eval, ok := sel.Harness.(harness.EvalRunner)
	if !ok {
		return nil, fmt.Errorf("judge harness %s cannot run headless judge sessions", sel.Harness.ID())
	}
	cli, ok := harness.Available(sel.Harness)
	if !ok {
		return nil, fmt.Errorf("judge harness %s: CLI not found on PATH", sel.Harness.ID())
	}
	return &HarnessJudge{sel: sel, eval: eval, cli: cli, runner: r, hostSandboxed: hostSandboxed}, nil
}

// Judge runs one judge session in ws and returns the judge's final response
// text (ANSI-stripped, like eval output).
func (j *HarnessJudge) Judge(ctx context.Context, ws, prompt string, timeout time.Duration) (string, error) {
	cliModelID, _ := j.sel.Model.CLIModelID(j.sel.Harness.ID())
	spec := j.eval.JudgeSpec(ws, model.JudgeInput{Prompt: prompt, HostSandboxed: j.hostSandboxed}, cliModelID)
	spec.Argv[0] = j.cli
	res, err := j.runner.Run(ctx, spec, timeout, nil)
	if err != nil {
		return "", err
	}
	if res.TimedOut {
		return "", errors.New("timed out")
	}
	if reason := j.eval.RuntimeError(res.Stdout, res.ExitCode, res.TimedOut); reason != "" {
		return "", errors.New(reason)
	}
	text, _ := j.eval.ParseEvalOutput(res.Stdout)
	return ansi.Strip(text), nil
}

// UnavailableJudge fails every verdict with the resolution failure that made
// the (defaulted, never explicitly configured) judge unrunnable — the sweep
// still runs, and only llm assertions error.
type UnavailableJudge struct{ Reason string }

// Judge always errors with the resolution failure reason.
func (u UnavailableJudge) Judge(context.Context, string, string, time.Duration) (string, error) {
	return "", errors.New("judge unavailable: " + u.Reason)
}
