// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/grade"
	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/results"
)

// sessionRunner drives one eval's agent session in its staged workspace. The
// error return is reserved for aborting the sweep (an executor failure or a
// cancelled context); an agent that ran but produced nothing gradable comes
// back as a session with runtimeError set.
type sessionRunner interface {
	run(ctx context.Context, ws string, c evalspec.Eval, timeout time.Duration, maxTurns int) (agentSession, error)
}

// agentSession is what one agent session produced, whatever drove it: the
// graded output and evidence, the run log retained beside the workspace, and
// — when the run produced nothing gradable — the runtime failure.
type agentSession struct {
	output    string
	toolCalls []model.ToolCall // nil when the executor cannot report them
	measured  *results.Measured
	elapsed   time.Duration
	log       []byte

	// runtimeError is the recorded reason the run is ungradable; failure and
	// failureDetail render it for the live output ("<failure>: <detail>").
	runtimeError  string
	failure       string
	failureDetail string

	// extend adds executor-specific grading signals (a hosted conversation's
	// state, HITL requests, outcome, violations, judge context); nil adds none.
	extend func(*grade.Options)
	// conversation summarizes a hosted agent's conversation for the results
	// file; nil for a CLI session.
	conversation *results.Conversation
}

// cliSession runs an eval through a local agent CLI: the harness's headless
// eval command, executed by the sweep's Runner.
type cliSession struct {
	opts       EvalOptions
	sel        harness.Selection
	evalRunner harness.EvalRunner
	cli        string
}

func newCLISession(opts EvalOptions, sel harness.Selection) *cliSession {
	evalRunner, _ := sel.Harness.(harness.EvalRunner)
	cli, _ := harness.Available(sel.Harness)
	return &cliSession{opts: opts, sel: sel, evalRunner: evalRunner, cli: cli}
}

func (s *cliSession) run(ctx context.Context, ws string, c evalspec.Eval, timeout time.Duration,
	maxTurns int,
) (agentSession, error) {
	cliModelID, _ := s.sel.Model.CLIModelID(s.sel.Harness.ID())
	spec := s.evalRunner.EvalSpec(ws, model.EvalInput{
		Prompt:        c.Prompt,
		MaxTurns:      maxTurns,
		HostSandboxed: s.opts.HostSandboxed,
	}, cliModelID)
	spec.Argv[0] = s.cli

	res, err := s.opts.Runner.Run(ctx, spec, timeout, nil)
	if err != nil {
		return agentSession{}, err
	}
	if res.TimedOut {
		s.opts.reporter().Warn("  warn: %s timed out after %s; grading partial output\n", s.cli, timeout)
	}
	sess := agentSession{elapsed: res.Elapsed, log: res.Stdout}
	if reason := s.evalRunner.RuntimeError(res.Stdout, res.ExitCode, res.TimedOut); reason != "" {
		sess.runtimeError = reason
		sess.failure = fmt.Sprintf("%s runtime failure (exit %d)", s.cli, res.ExitCode)
		sess.failureDetail = errorDetail(reason, res.StderrTail)
		return sess, nil
	}

	// Providers that decode the agent's answer out of a JSON envelope
	// re-materialize any ANSI a tool printed (carried backslash-u escaped in
	// the JSON, so untouched by the runner's capture-time strip) back into raw
	// escape bytes; strip the decoded text so graded evidence and the TUI
	// output stay plain.
	output, usage := s.evalRunner.ParseEvalOutput(res.Stdout)
	sess.output = ansi.Strip(output)
	sess.measured = measured(s.sel.Model, usage)
	// Tool calls drive tool_call assertions and the execution metrics.
	sess.toolCalls = observedToolCalls(s.sel.Harness, res.Stdout)
	return sess, nil
}
