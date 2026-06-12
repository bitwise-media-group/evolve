// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package grade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/runner"
)

const judgePrompt = `You are grading an AI coding agent's work. Assertion to verify:

%s

The agent's final response was:
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
	Run(ctx context.Context, spec provider.CommandSpec, timeout time.Duration,
		onLine func([]byte) bool) (runner.Result, error)
}

// Options configures grading for one case.
type Options struct {
	Runner     Runner
	Workspace  string        // the case's throwaway workspace
	Output     string        // the agent's final response text
	Timeout    time.Duration // shared by command assertions and the judge
	JudgeModel string        // "" = DefaultJudgeModel
}

// Assertion grades one assertion. passed is tri-state: nil means skipped
// (e.g. a required binary is not installed).
func Assertion(ctx context.Context, a evalspec.Assertion, opts Options) (passed *bool, evidence string) {
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
		res, err := opts.Runner.Run(ctx, provider.CommandSpec{
			Argv: []string{"/bin/sh", "-c", a.Run},
			Dir:  cwd,
		}, opts.Timeout, nil)
		if err != nil {
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

	case "llm":
		verdict, evidence := judge(ctx, a.Text, opts)
		return &verdict, evidence
	}

	f := false
	return &f, "unknown assertion type: " + a.Type
}

// judge asks the claude CLI for a verdict; any failure to obtain a parseable
// one fails the assertion loudly.
func judge(ctx context.Context, assertion string, opts Options) (bool, string) {
	model := opts.JudgeModel
	if model == "" {
		model = DefaultJudgeModel
	}
	prompt := fmt.Sprintf(judgePrompt, assertion, truncate(opts.Output, 8000), opts.Workspace)
	res, err := opts.Runner.Run(ctx, provider.CommandSpec{
		Argv: []string{"claude", "-p", prompt,
			"--model", model,
			"--output-format", "json",
			"--max-turns", "4",
			"--allowedTools", "Read Glob Grep"},
		Dir: opts.Workspace,
	}, opts.Timeout, nil)
	if err != nil {
		return false, fmt.Sprintf("judge error: %v", err)
	}
	if res.TimedOut {
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
