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
	"strings"
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

const judgePrompt = `You are grading an AI coding agent's work against %d numbered assertions.

Assertions to verify:
%s
%sThe agent's final response was:
---
%s
---

The agent's workspace is at: %s
You may inspect any files in it to verify the assertions.

Grade every assertion independently. Reply with ONLY a JSON object of exactly this shape:
{"verdicts": [{"id": 1, "passed": true, "evidence": "<short quote or file fact supporting the verdict>"}, ...]}
Include exactly one verdict for every assertion id, 1 through %d.`

// DefaultJudgeModel is the LLM-judge model (a canonical registry id; a bare id
// also resolves) used when neither the judge_model config key nor
// --judge-model names one. The model should be consistent across runs so
// verdicts stay comparable between them and the providers under test; changing
// it re-bases every subsequent verdict.
const DefaultJudgeModel = "anthropic/claude-sonnet-5"

// Runner runs grading subprocesses (shell command assertions).
// scan is always nil here (collect mode); the *runner.Scan shape matches the
// agent runner so one Exec implementation serves both.
type Runner interface {
	Run(ctx context.Context, spec model.CommandSpec, timeout time.Duration,
		scan *runner.Scan) (runner.Result, error)
}

// Judge obtains LLM verdicts: it runs one judge session in the eval workspace
// — a single session grades all of a case's llm assertions — and returns the
// judge's final response text. Implemented by internal/run over a resolved
// (model, harness) selection; grade stays harness-free.
type Judge interface {
	Judge(ctx context.Context, ws, prompt string, timeout time.Duration) (string, error)
}

// Options configures grading for one eval.
type Options struct {
	Runner         Runner
	Workspace      string        // the eval's throwaway workspace
	Output         string        // the agent's final response text
	ExpectedOutput string        // the eval author's success description, judge context only
	Timeout        time.Duration // shared by command assertions and the judge
	// Judge grades llm assertions; nil fails them loudly.
	Judge Judge
	// ToolCalls are the agent's observed tool invocations; nil when the harness
	// cannot report them (a tool_call assertion is then skipped), a non-nil
	// empty slice when it reported zero (the assertion fails).
	ToolCalls []model.ToolCall
}

// Verdict is one graded assertion's outcome, index-aligned with the input.
// Passed is tri-state: nil means skipped (e.g. a required binary is not
// installed).
type Verdict struct {
	Passed   *bool
	Evidence string
}

// Case grades all of one eval's assertions: deterministic checks first (the
// judge runs with the full eval tool posture and may mutate the workspace it
// inspects, so it must never run before evidence is collected), then every
// llm assertion in a single judge session. The returned verdicts are
// index-aligned with as, so results stay in authored order regardless of
// grading order.
func Case(ctx context.Context, as []evalspec.Assertion, opts Options) []Verdict {
	verdicts := make([]Verdict, len(as))
	var llm []evalspec.Assertion
	var llmIdx []int
	for i, a := range as {
		if a.Type == "llm" {
			llm = append(llm, a)
			llmIdx = append(llmIdx, i)
			continue
		}
		passed, evidence := one(ctx, a, opts)
		verdicts[i] = Verdict{Passed: passed, Evidence: evidence}
	}
	for j, v := range judgeBatch(ctx, llm, opts) {
		verdicts[llmIdx[j]] = v
	}
	return verdicts
}

// one grades one non-llm assertion. passed is tri-state: nil means skipped
// (e.g. a required binary is not installed).
func one(ctx context.Context, a evalspec.Assertion, opts Options) (passed *bool, evidence string) {
	// Only the command branch shells out (an agent.exec child span and real
	// latency); the deterministic file/regex checks are too cheap to span.
	if a.Type == "command" {
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

// judgeBatch grades every llm assertion of a case in one judge session,
// returning verdicts index-aligned with llm. Failure to obtain a parseable
// verdict fails the affected entries loudly, per entry: a session-level
// failure (no judge, error, no envelope) fails them all, a missing id fails
// only that entry.
func judgeBatch(ctx context.Context, llm []evalspec.Assertion, opts Options) []Verdict {
	if len(llm) == 0 {
		return nil
	}
	ctx, span := tracer().Start(ctx, "evolve.grade.assertion",
		trace.WithAttributes(
			attribute.String("assertion_type", "llm"),
			attribute.Int("assertion_count", len(llm))))
	defer span.End()
	verdicts := judgeBatchVerdicts(ctx, llm, opts)
	for i, v := range verdicts {
		attrs := []attribute.KeyValue{attribute.Int("id", i+1)}
		if v.Passed != nil {
			attrs = append(attrs, attribute.Bool("passed", *v.Passed))
		}
		span.AddEvent("verdict", trace.WithAttributes(attrs...))
	}
	return verdicts
}

// judgeBatchVerdicts builds the batch prompt, runs the single judge session,
// and maps the returned 1-based verdict ids back onto the llm slice.
func judgeBatchVerdicts(ctx context.Context, llm []evalspec.Assertion, opts Options) []Verdict {
	failAll := func(evidence string) []Verdict {
		out := make([]Verdict, len(llm))
		for i := range out {
			f := false
			out[i] = Verdict{Passed: &f, Evidence: evidence}
		}
		return out
	}
	if opts.Judge == nil {
		return failAll("judge error: no judge configured")
	}
	var block strings.Builder
	for i, a := range llm {
		fmt.Fprintf(&block, "%d. %s\n", i+1, a.Text)
	}
	expected := "\n"
	if opts.ExpectedOutput != "" {
		expected = "\nThe eval author's description of the expected output (context, not a " +
			"separate assertion):\n---\n" + truncate(opts.ExpectedOutput, 2000) + "\n---\n\n"
	}
	prompt := fmt.Sprintf(judgePrompt, len(llm), block.String(), expected,
		truncate(opts.Output, 8000), opts.Workspace, len(llm))
	text, err := opts.Judge.Judge(ctx, opts.Workspace, prompt, opts.Timeout)
	if err != nil {
		slog.DebugContext(ctx, "judge error", slog.Any("error", err))
		return failAll(fmt.Sprintf("judge error: %v", err))
	}
	byID, ok := parseVerdicts(text)
	if !ok {
		return failAll("judge error: no JSON verdicts in response")
	}
	out := make([]Verdict, len(llm))
	for i := range llm {
		v, ok := byID[i+1]
		if !ok {
			f := false
			out[i] = Verdict{Passed: &f, Evidence: fmt.Sprintf("judge error: no verdict for assertion %d", i+1)}
			continue
		}
		passed := v.passed
		out[i] = Verdict{Passed: &passed, Evidence: truncate(v.evidence, 200)}
	}
	return out
}

// verdict is one parsed entry of the judge's verdicts envelope.
type verdict struct {
	passed   bool
	evidence string
}

// parseVerdicts extracts the JSON verdicts envelope from the judge's response
// text. The judge may wrap the object in prose or code fences, so every "{"
// offset is tried as a decode start (a json.Decoder stops at the object's end,
// so trailing text never breaks the decode); the first decode yielding a
// non-empty verdicts array wins. Duplicate ids first-win; ids outside 1..n are
// simply never looked up.
func parseVerdicts(text string) (map[int]verdict, bool) {
	for i := range len(text) {
		if text[i] != '{' {
			continue
		}
		var envelope struct {
			Verdicts []struct {
				ID       int  `json:"id"`
				Passed   bool `json:"passed"`
				Evidence any  `json:"evidence"`
			} `json:"verdicts"`
		}
		if json.NewDecoder(strings.NewReader(text[i:])).Decode(&envelope) != nil ||
			len(envelope.Verdicts) == 0 {
			continue
		}
		byID := make(map[int]verdict, len(envelope.Verdicts))
		for _, v := range envelope.Verdicts {
			if _, dup := byID[v.ID]; dup {
				continue
			}
			evidence := ""
			if v.Evidence != nil {
				evidence = fmt.Sprint(v.Evidence)
			}
			byID[v.ID] = verdict{passed: v.Passed, evidence: evidence}
		}
		return byID, true
	}
	return nil, false
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
