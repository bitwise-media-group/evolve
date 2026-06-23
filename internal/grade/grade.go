// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package grade

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/runner"
)

// scopeName is this package's OpenTelemetry instrumentation scope.
const scopeName = "github.com/bitwise-media-group/evolve/internal/grade"

func tracer() trace.Tracer { return otel.Tracer(scopeName) }

const judgePrompt = `You are grading an AI coding agent's work. Assertion to verify:

%s
%sThe agent's final response was:
---
%s
---

Files in the agent's workspace are at: %s
Reply with ONLY a JSON object: {"passed": true|false, "evidence": "<short quote or file fact supporting the verdict>"}`

// DefaultJudgeModel pins LLM-judge grading to one model so verdicts stay
// comparable across runs and providers under test.
const DefaultJudgeModel = "claude-sonnet-4-6"

// Runner runs grading subprocesses (shell commands and the judge CLI).
type Runner interface {
	Run(ctx context.Context, spec model.CommandSpec, timeout time.Duration,
		onLine func([]byte) bool) (runner.Result, error)
}

// Options configures grading for one eval.
type Options struct {
	Runner         Runner
	Workspace      string        // the eval's throwaway workspace
	Output         string        // the agent's final response text
	ExpectedOutput string        // the eval author's success description, judge context only
	Timeout        time.Duration // shared by command assertions and the judge
	JudgeModel     string        // "" = DefaultJudgeModel
	// ToolCalls are the agent's observed tool invocations; nil when the harness
	// cannot report them (a tool_call assertion is then skipped), a non-nil
	// empty slice when it reported zero (the assertion fails).
	ToolCalls []model.ToolCall
}

// Assertion grades one assertion. passed is tri-state: nil means skipped
// (e.g. a required binary is not installed).
func Assertion(ctx context.Context, a evalspec.Assertion, opts Options) (passed *bool, evidence string) {
	// Only the command and llm branches shell out (an agent.exec child span and
	// real latency); the deterministic file/regex checks are too cheap to span.
	if a.Type == "command" || a.Type == "llm" {
		var span trace.Span
		ctx, span = tracer().Start(ctx, "evolve.grade.assertion",
			trace.WithAttributes(attribute.String("assertion_type", a.Type)))
		defer func() {
			if passed != nil {
				span.SetAttributes(attribute.Bool("passed", *passed))
			}
			span.End()
		}()
	}
	switch a.Type {
	case "file_exists", "file_absent":
		_, err := os.Stat(filepath.Join(opts.Workspace, a.Path))
		exists := err == nil
		verdict := exists
		if a.Type == "file_absent" {
			verdict = !exists
		}
		state := "missing"
		if exists {
			state = "exists"
		}
		return &verdict, fmt.Sprintf("%s %s", a.Path, state)

	case "regex", "not_regex":
		text := opts.Output
		if a.Path != "" {
			data, err := os.ReadFile(filepath.Join(opts.Workspace, a.Path))
			if err != nil {
				f := false
				return &f, a.Path + " missing"
			}
			text = string(data)
		}
		re, err := regexp.Compile("(?m)" + a.Pattern)
		if err != nil {
			f := false
			return &f, fmt.Sprintf("invalid pattern: %v", err)
		}
		match := re.FindString(text)
		matched := re.MatchString(text)
		verdict := matched
		if a.Type == "not_regex" {
			verdict = !matched
		}
		evidence = "no match"
		if matched {
			evidence = truncate(match, 120)
		}
		return &verdict, evidence

	case "command":
		if a.Requires != "" {
			if _, err := exec.LookPath(a.Requires); err != nil {
				return nil, "skipped: " + a.Requires + " not installed"
			}
		}
		cwd := opts.Workspace
		if a.Cwd != "" {
			cwd = filepath.Join(opts.Workspace, a.Cwd)
		}
		res, err := opts.Runner.Run(ctx, model.CommandSpec{
			Argv: []string{"/bin/sh", "-c", a.Run},
			Dir:  cwd,
		}, opts.Timeout, nil)
		if err != nil {
			slog.DebugContext(ctx, "grade command error",
				slog.String("run", a.Run),
				slog.Any("error", err))
			f := false
			return &f, fmt.Sprintf("command error: %v", err)
		}
		expected := 0
		if a.ExpectExit != nil {
			expected = *a.ExpectExit
		}
		verdict := res.ExitCode == expected
		combined := string(res.Stdout) + res.StderrTail
		return &verdict, fmt.Sprintf("exit %d: %s", res.ExitCode, tail(combined, 200))

	case "tool_call":
		return gradeToolCall(a, opts.ToolCalls)

	case "llm":
		verdict, evidence := judge(ctx, a.Text, opts)
		return &verdict, evidence
	}

	f := false
	return &f, "unknown assertion type: " + a.Type
}

// gradeToolCall passes when some observed tool call's name matches a.Tool and,
// when a.Pattern is set, its JSON-serialized arguments match that too. nil calls
// means the harness cannot report tool calls (the assertion is skipped); a
// non-nil empty slice means it reported none (the assertion fails).
func gradeToolCall(a evalspec.Assertion, calls []model.ToolCall) (*bool, string) {
	if calls == nil {
		return nil, "skipped: harness does not report tool calls"
	}
	nameRE, err := regexp.Compile(a.Tool)
	if err != nil {
		f := false
		return &f, fmt.Sprintf("invalid tool pattern: %v", err)
	}
	var argsRE *regexp.Regexp
	if a.Pattern != "" {
		if argsRE, err = regexp.Compile(a.Pattern); err != nil {
			f := false
			return &f, fmt.Sprintf("invalid pattern: %v", err)
		}
	}
	for _, tc := range calls {
		if !nameRE.MatchString(tc.Name) {
			continue
		}
		if argsRE != nil && !argsRE.MatchString(string(tc.Input)) {
			continue
		}
		verdict := true
		evidence := tc.Name
		if len(tc.Input) > 0 {
			evidence += " " + truncate(string(tc.Input), 120)
		}
		return &verdict, evidence
	}
	f := false
	return &f, "no tool_call matched /" + a.Tool + "/"
}

// judge asks the claude CLI for a verdict; any failure to obtain a parseable
// one fails the assertion loudly.
func judge(ctx context.Context, assertion string, opts Options) (bool, string) {
	judgeModel := opts.JudgeModel
	if judgeModel == "" {
		judgeModel = DefaultJudgeModel
	}
	expected := "\n"
	if opts.ExpectedOutput != "" {
		expected = "\nThe eval author's description of the expected output (context, not a " +
			"separate assertion):\n---\n" + truncate(opts.ExpectedOutput, 2000) + "\n---\n\n"
	}
	prompt := fmt.Sprintf(judgePrompt, assertion, expected, truncate(opts.Output, 8000), opts.Workspace)
	res, err := opts.Runner.Run(ctx, model.CommandSpec{
		Argv: []string{"claude", "-p", prompt,
			"--model", judgeModel,
			"--output-format", "json",
			"--max-turns", "4",
			"--allowedTools", "Read Glob Grep"},
		Dir: opts.Workspace,
	}, opts.Timeout, nil)
	if err != nil {
		slog.DebugContext(ctx, "judge error",
			slog.String("model", judgeModel),
			slog.Any("error", err))
		return false, fmt.Sprintf("judge error: %v", err)
	}
	if res.TimedOut {
		slog.DebugContext(ctx, "judge timed out", slog.String("model", judgeModel))
		return false, "judge error: timed out"
	}

	var payload struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(res.Stdout, &payload); err != nil {
		return false, fmt.Sprintf("judge error: unparseable CLI output: %v", err)
	}
	raw := regexp.MustCompile(`(?s)\{.*\}`).FindString(payload.Result)
	if raw == "" {
		return false, "judge error: no JSON verdict in response"
	}
	var verdict struct {
		Passed   bool `json:"passed"`
		Evidence any  `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		return false, fmt.Sprintf("judge error: invalid verdict: %v", err)
	}
	evidence := ""
	if verdict.Evidence != nil {
		evidence = fmt.Sprint(verdict.Evidence)
	}
	return verdict.Passed, truncate(evidence, 200)
}

// Describe renders an assertion as the human-readable statement that results
// files carry as the expectation text (grading.json's expectations[].text).
// The templates are stable: committed results diff only when grading does.
func Describe(a evalspec.Assertion) string {
	switch a.Type {
	case "file_exists":
		return "file " + a.Path + " exists"
	case "file_absent":
		return "file " + a.Path + " is absent"
	case "regex":
		if a.Path != "" {
			return a.Path + " matches /" + a.Pattern + "/"
		}
		return "output matches /" + a.Pattern + "/"
	case "not_regex":
		if a.Path != "" {
			return a.Path + " does not match /" + a.Pattern + "/"
		}
		return "output does not match /" + a.Pattern + "/"
	case "command":
		exit := 0
		if a.ExpectExit != nil {
			exit = *a.ExpectExit
		}
		return fmt.Sprintf("command `%s` exits %d", a.Run, exit)
	case "tool_call":
		if a.Pattern != "" {
			return "agent called tool /" + a.Tool + "/ with args /" + a.Pattern + "/"
		}
		return "agent called tool /" + a.Tool + "/"
	case "llm":
		return a.Text
	}
	return a.Type
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
