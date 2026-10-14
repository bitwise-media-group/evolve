// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package grade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/bitwise-media-group/evolve/internal/agui"
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
%s
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
	// cannot report them (tool_call and tool_sequence assertions are then
	// skipped), a non-nil empty slice when it reported zero (they fail).
	ToolCalls []model.ToolCall

	// The hosted-agent conversation's signals, each nil (or "") when the run
	// has none — a CLI agent — so its assertions skip. State is the final
	// shared state; HITL the human-in-the-loop requests (non-nil empty when
	// there were none); Outcome how the conversation ended; Violations the
	// error-severity protocol-conformance violations (non-nil empty when
	// conformant).
	State      json.RawMessage
	HITL       []model.HITLRequest
	Outcome    string
	Violations []string
	// JudgeContext, when set, is appended to the judge prompt — e.g. that the
	// agent ran remotely and where its transcript is in the workspace.
	JudgeContext string
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
	case "tool_sequence":
		return gradeToolSequence(a, opts.ToolCalls)
	case "state":
		return gradeState(a, opts.State)
	case "hitl":
		return gradeHITL(a, opts.HITL)
	case "outcome":
		return gradeOutcome(a, opts.Outcome)
	case "conformance":
		return gradeConformance(opts.Violations)
	}

	f := false
	return &f, "unknown assertion type: " + a.Type
}

// gradeOutcome passes when the conversation ended as a.Expect; "" (no
// hosted conversation) skips.
func gradeOutcome(a evalspec.Assertion, outcome string) (*bool, string) {
	if outcome == "" {
		return nil, "skipped: not a hosted-agent conversation"
	}
	verdict := outcome == a.Expect
	return &verdict, "conversation ended " + outcome
}

// gradeConformance passes when there are no error-severity protocol
// violations; nil (no hosted conversation) skips.
func gradeConformance(violations []string) (*bool, string) {
	if violations == nil {
		return nil, "skipped: not a hosted-agent conversation"
	}
	verdict := len(violations) == 0
	if verdict {
		return &verdict, "no protocol violations"
	}
	return &verdict, fmt.Sprintf("%d %s: %s", len(violations), plural(len(violations), "violation", "violations"),
		truncate(strings.Join(violations, "; "), 200))
}

// gradeToolCall counts the observed calls whose name matches a.Tool and,
// when set, whose JSON-serialized arguments match a.Pattern and whose result
// matches a.Result, and passes when the count is within [min, max]: min
// defaults to 1 (0 when max is 0, so max: 0 asserts the tool was never
// called) and max to unbounded. nil calls means the harness cannot report
// tool calls (the assertion is skipped); a non-nil empty slice means it
// reported none.
func gradeToolCall(a evalspec.Assertion, calls []model.ToolCall) (*bool, string) {
	if calls == nil {
		return nil, "skipped: harness does not report tool calls"
	}
	nameRE, argsRE, resultRE, err := compileToolCall(a)
	if err != nil {
		f := false
		return &f, err.Error()
	}
	var matched []model.ToolCall
	for _, tc := range calls {
		if !nameRE.MatchString(tc.Name) || (argsRE != nil && !argsRE.MatchString(string(tc.Input))) {
			continue
		}
		if resultRE != nil && (tc.Result == nil || !resultRE.MatchString(*tc.Result)) {
			continue
		}
		matched = append(matched, tc)
	}
	lo, hi := bounds(a)
	verdict := len(matched) >= lo && (hi < 0 || len(matched) <= hi)
	switch {
	case len(matched) == 0:
		return &verdict, "no tool_call matched /" + a.Tool + "/"
	case a.Min == nil && a.Max == nil && verdict:
		return &verdict, callEvidence(matched[0])
	}
	return &verdict, fmt.Sprintf("%d matching %s (%s): %s", len(matched), plural(len(matched), "call", "calls"),
		boundsText(lo, hi), callEvidence(matched[0]))
}

// compileToolCall compiles a tool_call's name, args, and result patterns
// (nil when unset).
func compileToolCall(a evalspec.Assertion) (nameRE, argsRE, resultRE *regexp.Regexp, err error) {
	if nameRE, err = regexp.Compile(a.Tool); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid tool pattern: %v", err)
	}
	if a.Pattern != "" {
		if argsRE, err = regexp.Compile(a.Pattern); err != nil {
			return nil, nil, nil, fmt.Errorf("invalid pattern: %v", err)
		}
	}
	if a.Result != "" {
		if resultRE, err = regexp.Compile(a.Result); err != nil {
			return nil, nil, nil, fmt.Errorf("invalid result pattern: %v", err)
		}
	}
	return nameRE, argsRE, resultRE, nil
}

// callEvidence renders one call as evidence: its name and (truncated) args.
func callEvidence(tc model.ToolCall) string {
	if len(tc.Input) == 0 {
		return tc.Name
	}
	return tc.Name + " " + truncate(string(tc.Input), 120)
}

// bounds resolves a match-count assertion's [min, max]; max -1 is unbounded.
// min defaults to 1, or 0 when max is 0.
func bounds(a evalspec.Assertion) (lo, hi int) {
	lo, hi = 1, -1
	if a.Max != nil {
		hi = *a.Max
		if hi == 0 {
			lo = 0
		}
	}
	if a.Min != nil {
		lo = *a.Min
	}
	return lo, hi
}

// boundsText renders [lo, hi] for evidence and descriptions.
func boundsText(lo, hi int) string {
	switch {
	case hi == 0:
		return "never"
	case hi < 0:
		return fmt.Sprintf("at least %d", lo)
	case lo == hi:
		return fmt.Sprintf("exactly %d", lo)
	case lo == 0:
		return fmt.Sprintf("at most %d", hi)
	}
	return fmt.Sprintf("between %d and %d", lo, hi)
}

// boundsCount is boundsText with a counted noun ("exactly 1 time", "at least
// 2 requests"); "never" stands alone.
func boundsCount(lo, hi int, one, many string) string {
	if hi == 0 {
		return "never"
	}
	n := hi
	if hi < 0 {
		n = lo
	}
	return boundsText(lo, hi) + " " + plural(n, one, many)
}

// gradeToolSequence passes when the a.Tools patterns match an ordered
// subsequence of the observed calls' names (other calls may come between).
func gradeToolSequence(a evalspec.Assertion, calls []model.ToolCall) (*bool, string) {
	if calls == nil {
		return nil, "skipped: harness does not report tool calls"
	}
	res := make([]*regexp.Regexp, len(a.Tools))
	for i, expr := range a.Tools {
		re, err := regexp.Compile(expr)
		if err != nil {
			f := false
			return &f, fmt.Sprintf("invalid tools[%d] pattern: %v", i, err)
		}
		res[i] = re
	}
	var seen []string
	next := 0
	for _, tc := range calls {
		if next < len(res) && res[next].MatchString(tc.Name) {
			seen = append(seen, tc.Name)
			next++
		}
	}
	verdict := next == len(res)
	if verdict {
		return &verdict, strings.Join(seen, " → ")
	}
	evidence := fmt.Sprintf("matched %d/%d; no call to /%s/", next, len(res), a.Tools[next])
	if next > 0 {
		evidence += " after " + seen[next-1]
	}
	return &verdict, evidence
}

// gradeState checks the final shared state at a.Pointer: that it exists, or
// equals a.Equals (JSON equality), or (rendered as text: a string verbatim,
// anything else as compact JSON) matches a.Pattern.
func gradeState(a evalspec.Assertion, state json.RawMessage) (*bool, string) {
	if state == nil {
		return nil, "skipped: no shared state (not a hosted-agent conversation)"
	}
	f := false
	ptr, err := agui.ParsePointer(a.Pointer)
	if err != nil {
		return &f, err.Error()
	}
	var doc any
	if err := json.Unmarshal(state, &doc); err != nil {
		return &f, fmt.Sprintf("final state is not JSON: %v", err)
	}
	where := "state" + a.Pointer
	v, ok := ptr.Get(doc)
	if !ok {
		return &f, where + " is absent"
	}
	text := jsonText(v)
	switch {
	case a.Equals != nil:
		var want any
		if err := json.Unmarshal(a.Equals, &want); err != nil {
			return &f, fmt.Sprintf("invalid equals: %v", err)
		}
		verdict := reflect.DeepEqual(v, want)
		if verdict {
			return &verdict, where + " = " + truncate(text, 120)
		}
		return &verdict, fmt.Sprintf("%s = %s, want %s", where, truncate(text, 80), truncate(jsonText(want), 80))
	case a.Pattern != "":
		re, err := regexp.Compile("(?m)" + a.Pattern)
		if err != nil {
			return &f, fmt.Sprintf("invalid pattern: %v", err)
		}
		verdict := re.MatchString(text)
		return &verdict, where + " = " + truncate(text, 120)
	}
	t := true
	return &t, where + " = " + truncate(text, 120)
}

// jsonText renders a decoded JSON value as text: a string verbatim, anything
// else as compact JSON.
func jsonText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	data, _ := json.Marshal(v)
	return string(data)
}

// gradeHITL counts the conversation's human-in-the-loop requests that match
// — a.Tool on the requested tool's name, a.Reason on an interrupt's reason
// (only interrupts count then), a.Pattern on the tool arguments or interrupt
// message — and passes when the count is within [min, max], defaulting like
// tool_call. nil requests means no hosted conversation (skipped).
func gradeHITL(a evalspec.Assertion, reqs []model.HITLRequest) (*bool, string) {
	if reqs == nil {
		return nil, "skipped: not a hosted-agent conversation"
	}
	f := false
	var toolRE, reasonRE, patternRE *regexp.Regexp
	for _, p := range []struct {
		dst  **regexp.Regexp
		expr string
		name string
	}{{&toolRE, a.Tool, "tool"}, {&reasonRE, a.Reason, "reason"}, {&patternRE, a.Pattern, "pattern"}} {
		if p.expr == "" {
			continue
		}
		re, err := regexp.Compile(p.expr)
		if err != nil {
			return &f, fmt.Sprintf("invalid %s pattern: %v", p.name, err)
		}
		*p.dst = re
	}
	var matched []model.HITLRequest
	for _, r := range reqs {
		if toolRE != nil && !toolRE.MatchString(r.Name) {
			continue
		}
		if reasonRE != nil && (r.Kind != "interrupt" || !reasonRE.MatchString(r.Reason)) {
			continue
		}
		if patternRE != nil && !patternRE.MatchString(r.Detail) {
			continue
		}
		matched = append(matched, r)
	}
	lo, hi := bounds(a)
	verdict := len(matched) >= lo && (hi < 0 || len(matched) <= hi)
	if len(matched) == 0 {
		return &verdict, fmt.Sprintf("no matching human-in-the-loop request (%d in total)", len(reqs))
	}
	first := matched[0]
	label := first.Name
	if first.Kind == "interrupt" && first.Reason != "" {
		label = first.Reason
		if first.Name != "" {
			label += " (" + first.Name + ")"
		}
	}
	return &verdict, fmt.Sprintf("%d matching %s (%s): %s %s", len(matched), plural(len(matched), "request", "requests"),
		boundsText(lo, hi), first.Kind, label)
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
	judgeContext := ""
	if opts.JudgeContext != "" {
		judgeContext = "\n" + opts.JudgeContext + "\n"
	}
	prompt := fmt.Sprintf(judgePrompt, len(llm), block.String(), expected,
		truncate(opts.Output, 8000), opts.Workspace, judgeContext, len(llm))
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
		return describeToolCall(a)
	case "tool_sequence":
		seq := make([]string, len(a.Tools))
		for i, t := range a.Tools {
			seq[i] = "/" + t + "/"
		}
		return "agent called tools in order: " + strings.Join(seq, " → ")
	case "state":
		where := "state" + a.Pointer
		switch {
		case a.Equals != nil:
			return where + " equals " + compactJSON(a.Equals)
		case a.Pattern != "":
			return where + " matches /" + a.Pattern + "/"
		}
		return where + " exists"
	case "hitl":
		s := "agent asked for human input"
		if a.Tool != "" {
			s += " via tool /" + a.Tool + "/"
		}
		if a.Reason != "" {
			s += " with reason /" + a.Reason + "/"
		}
		if a.Pattern != "" {
			s += " matching /" + a.Pattern + "/"
		}
		if a.Min != nil || a.Max != nil {
			lo, hi := bounds(a)
			s += " (" + boundsCount(lo, hi, "request", "requests") + ")"
		}
		return s
	case "outcome":
		return "conversation ended " + a.Expect
	case "conformance":
		return "agent's event stream conforms to the protocol"
	case "llm":
		return a.Text
	}
	return a.Type
}

// describeToolCall renders a tool_call; one without the count/result fields
// keeps its original wording so committed expectation text does not churn.
func describeToolCall(a evalspec.Assertion) string {
	verb := "called"
	lo, hi := bounds(a)
	if hi == 0 {
		verb = "never called"
	}
	s := "agent " + verb + " tool /" + a.Tool + "/"
	if a.Pattern != "" {
		s += " with args /" + a.Pattern + "/"
	}
	if a.Result != "" {
		s += " returning /" + a.Result + "/"
	}
	if (a.Min != nil || a.Max != nil) && hi != 0 {
		s += " (" + boundsCount(lo, hi, "time", "times") + ")"
	}
	return s
}

// compactJSON renders raw JSON on one line (verbatim if it does not parse).
func compactJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return string(raw)
	}
	return buf.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
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
