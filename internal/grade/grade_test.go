// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package grade

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/runner"
)

// fakeJudge returns a canned judge response and records what it was asked.
// onJudge, when set, observes the workspace at judge time (ordering tests).
type fakeJudge struct {
	response  string
	err       error
	calls     int
	gotWS     string
	gotPrompt string
	onJudge   func(ws string)
}

func (f *fakeJudge) Judge(_ context.Context, ws, prompt string, _ time.Duration) (string, error) {
	f.calls++
	f.gotWS, f.gotPrompt = ws, prompt
	if f.onJudge != nil {
		f.onJudge(ws)
	}
	return f.response, f.err
}

func opts(t *testing.T, output string) Options {
	t.Helper()
	return Options{
		Runner:    &runner.Exec{},
		Workspace: t.TempDir(),
		Output:    output,
		Timeout:   10 * time.Second,
		Judge:     &fakeJudge{},
	}
}

// caseOne grades a single assertion through Case.
func caseOne(t *testing.T, a evalspec.Assertion, o Options) (passed *bool, evidence string) {
	t.Helper()
	verdicts := Case(context.Background(), []evalspec.Assertion{a}, o)
	if len(verdicts) != 1 {
		t.Fatalf("Case returned %d verdicts, want 1", len(verdicts))
	}
	return verdicts[0].Passed, verdicts[0].Evidence
}

func boolish(p *bool) string {
	switch {
	case p == nil:
		return "skip"
	case *p:
		return "pass"
	default:
		return "fail"
	}
}

func TestFileAssertions(t *testing.T) {
	o := opts(t, "")
	os.WriteFile(filepath.Join(o.Workspace, "present.txt"), []byte("x"), 0o644)

	tests := []struct {
		a    evalspec.Assertion
		want string
	}{
		{evalspec.Assertion{Type: "file_exists", Path: "present.txt"}, "pass"},
		{evalspec.Assertion{Type: "file_exists", Path: "absent.txt"}, "fail"},
		{evalspec.Assertion{Type: "file_absent", Path: "absent.txt"}, "pass"},
		{evalspec.Assertion{Type: "file_absent", Path: "present.txt"}, "fail"},
	}
	for _, tt := range tests {
		passed, _ := caseOne(t, tt.a, o)
		if boolish(passed) != tt.want {
			t.Errorf("%+v = %s, want %s", tt.a, boolish(passed), tt.want)
		}
	}
}

func TestRegexAssertions(t *testing.T) {
	o := opts(t, "final output says DONE")
	os.WriteFile(filepath.Join(o.Workspace, "main.go"), []byte("func TestClamp(t *testing.T) {\n\tt.Run(\"x\", nil)\n}\n"), 0o644)

	tests := []struct {
		a            evalspec.Assertion
		want         string
		wantEvidence string
	}{
		{evalspec.Assertion{Type: "regex", Path: "main.go", Pattern: `t\.Run\(`}, "pass", "t.Run("},
		{evalspec.Assertion{Type: "regex", Path: "main.go", Pattern: `testify`}, "fail", "no match"},
		{evalspec.Assertion{Type: "regex", Path: "missing.go", Pattern: `x`}, "fail", "missing.go missing"},
		{evalspec.Assertion{Type: "not_regex", Path: "main.go", Pattern: `testify`}, "pass", "no match"},
		{evalspec.Assertion{Type: "regex", Pattern: `DONE`}, "pass", "DONE"}, // no path -> final output
		{evalspec.Assertion{Type: "not_regex", Pattern: `DONE`}, "fail", "DONE"},
	}
	for _, tt := range tests {
		passed, evidence := caseOne(t, tt.a, o)
		if boolish(passed) != tt.want || evidence != tt.wantEvidence {
			t.Errorf("%+v = (%s, %q), want (%s, %q)", tt.a, boolish(passed), evidence, tt.want, tt.wantEvidence)
		}
	}
}

func TestCommandAssertions(t *testing.T) {
	o := opts(t, "")
	os.WriteFile(filepath.Join(o.Workspace, "f.txt"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(o.Workspace, "sub"), 0o755)

	exitOne := 1
	tests := []struct {
		a    evalspec.Assertion
		want string
	}{
		{evalspec.Assertion{Type: "command", Run: "test -f f.txt"}, "pass"},
		{evalspec.Assertion{Type: "command", Run: "test -f nope.txt"}, "fail"},
		{evalspec.Assertion{Type: "command", Run: "test -f ../f.txt", Cwd: "sub"}, "pass"},
		{evalspec.Assertion{Type: "command", Run: "exit 1", ExpectExit: &exitOne}, "pass"},
		{evalspec.Assertion{Type: "command", Run: "true", Requires: "definitely-not-a-binary-zzz"}, "skip"},
	}
	for _, tt := range tests {
		passed, evidence := caseOne(t, tt.a, o)
		if boolish(passed) != tt.want {
			t.Errorf("%+v = (%s, %q), want %s", tt.a, boolish(passed), evidence, tt.want)
		}
	}
}

func TestToolCallAssertions(t *testing.T) {
	calls := []model.ToolCall{
		{Name: "Write", Input: json.RawMessage(`{"file_path":"foo.txt","content":"hello"}`)},
		{Name: "Bash", Input: json.RawMessage(`{"command":"terraform plan"}`)},
		{Name: "mcp__github__create_issue", Input: json.RawMessage(`{"title":"bug"}`)},
	}
	tests := []struct {
		name      string
		toolCalls []model.ToolCall
		a         evalspec.Assertion
		want      string
	}{
		{"name match", calls, evalspec.Assertion{Type: "tool_call", Tool: "Write"}, "pass"},
		{"name miss", calls, evalspec.Assertion{Type: "tool_call", Tool: "Edit"}, "fail"},
		{"name+args match", calls, evalspec.Assertion{Type: "tool_call", Tool: "Bash", Pattern: "terraform"}, "pass"},
		{"args miss", calls, evalspec.Assertion{Type: "tool_call", Tool: "Bash", Pattern: "kubectl"}, "fail"},
		{"mcp match", calls, evalspec.Assertion{Type: "tool_call", Tool: `mcp__github__.*`}, "pass"},
		{"invalid tool regex", calls, evalspec.Assertion{Type: "tool_call", Tool: "("}, "fail"},
		{"nil tool calls -> skip", nil, evalspec.Assertion{Type: "tool_call", Tool: "Write"}, "skip"},
		{"empty non-nil -> fail", []model.ToolCall{}, evalspec.Assertion{Type: "tool_call", Tool: "Write"}, "fail"},
	}
	for _, tt := range tests {
		o := opts(t, "")
		o.ToolCalls = tt.toolCalls
		passed, evidence := caseOne(t, tt.a, o)
		if boolish(passed) != tt.want {
			t.Errorf("%s: %+v = (%s, %q), want %s", tt.name, tt.a, boolish(passed), evidence, tt.want)
		}
	}
}

// TestLLMJudgeBatch pins the batched judge contract: one judge session grades
// every llm assertion of the case, the prompt numbers them 1-based in authored
// order, the envelope is extracted from surrounding prose/code fences, and the
// verdicts land index-aligned with distinct per-assertion evidence while
// deterministic entries grade as before.
func TestLLMJudgeBatch(t *testing.T) {
	o := opts(t, "the readme explains tradeoffs")
	os.WriteFile(filepath.Join(o.Workspace, "README.md"), []byte("x"), 0o644)
	j := o.Judge.(*fakeJudge)
	j.response = "Sure! Here is my grading:\n```json\n" +
		`{"verdicts": [
			{"id": 1, "passed": true, "evidence": "README covers omissions"},
			{"id": 2, "passed": false, "evidence": "no changelog entry"},
			{"id": 3, "passed": true, "evidence": "tests cover error paths"}
		]}` + "\n```\nLet me know if you need more detail."

	as := []evalspec.Assertion{
		{Type: "llm", Text: "README explains omissions"},
		{Type: "file_exists", Path: "README.md"},
		{Type: "llm", Text: "CHANGELOG mentions the fix"},
		{Type: "llm", Text: "tests cover error paths"},
	}
	verdicts := Case(context.Background(), as, o)

	wants := []struct {
		verdict  string
		evidence string
	}{
		{"pass", "README covers omissions"},
		{"pass", "README.md exists"},
		{"fail", "no changelog entry"},
		{"pass", "tests cover error paths"},
	}
	for i, want := range wants {
		if boolish(verdicts[i].Passed) != want.verdict || verdicts[i].Evidence != want.evidence {
			t.Errorf("verdict[%d] = (%s, %q), want (%s, %q)",
				i, boolish(verdicts[i].Passed), verdicts[i].Evidence, want.verdict, want.evidence)
		}
	}
	if j.calls != 1 {
		t.Errorf("judge calls = %d, want 1 (one session per case)", j.calls)
	}
	if j.gotWS != o.Workspace {
		t.Errorf("judge workspace = %q, want %q", j.gotWS, o.Workspace)
	}
	// The llm assertions are numbered 1-based in authored order, skipping the
	// deterministic entry between them.
	for _, line := range []string{
		"1. README explains omissions",
		"2. CHANGELOG mentions the fix",
		"3. tests cover error paths",
	} {
		if !strings.Contains(j.gotPrompt, line) {
			t.Errorf("judge prompt missing %q:\n%s", line, j.gotPrompt)
		}
	}
	if !strings.Contains(j.gotPrompt, "3 numbered assertions") {
		t.Errorf("judge prompt missing assertion count:\n%s", j.gotPrompt)
	}
}

func TestLLMJudgeErrorsFailLoudly(t *testing.T) {
	tests := []struct {
		name string
		set  func(o *Options)
	}{
		{"garbage response", func(o *Options) { o.Judge.(*fakeJudge).response = "total garbage" }},
		{"judge error", func(o *Options) { o.Judge.(*fakeJudge).err = errors.New("judge unavailable: no harness") }},
		{"nil judge", func(o *Options) { o.Judge = nil }},
	}
	for _, tt := range tests {
		o := opts(t, "x")
		os.WriteFile(filepath.Join(o.Workspace, "kept.txt"), []byte("x"), 0o644)
		tt.set(&o)
		verdicts := Case(context.Background(), []evalspec.Assertion{
			{Type: "llm", Text: "first"},
			{Type: "file_exists", Path: "kept.txt"},
			{Type: "llm", Text: "second"},
		}, o)
		// Every llm entry fails loudly; the deterministic entry still grades.
		for _, i := range []int{0, 2} {
			if boolish(verdicts[i].Passed) != "fail" || !strings.Contains(verdicts[i].Evidence, "judge error") {
				t.Errorf("%s: verdict[%d] = (%s, %q), want loud failure",
					tt.name, i, boolish(verdicts[i].Passed), verdicts[i].Evidence)
			}
		}
		if boolish(verdicts[1].Passed) != "pass" {
			t.Errorf("%s: deterministic verdict = %s, want pass", tt.name, boolish(verdicts[1].Passed))
		}
	}
}

// TestLLMJudgeMissingVerdict pins the per-entry loud-failure semantics: a
// missing id fails only that entry, and an id beyond the assertion count is
// ignored.
func TestLLMJudgeMissingVerdict(t *testing.T) {
	o := opts(t, "x")
	o.Judge.(*fakeJudge).response = `{"verdicts": [
		{"id": 1, "passed": true, "evidence": "a"},
		{"id": 3, "passed": true, "evidence": "c"},
		{"id": 4, "passed": true, "evidence": "extra"}
	]}`
	verdicts := Case(context.Background(), []evalspec.Assertion{
		{Type: "llm", Text: "one"},
		{Type: "llm", Text: "two"},
		{Type: "llm", Text: "three"},
	}, o)
	if boolish(verdicts[0].Passed) != "pass" || verdicts[0].Evidence != "a" {
		t.Errorf("verdict[0] = (%s, %q), want (pass, a)", boolish(verdicts[0].Passed), verdicts[0].Evidence)
	}
	if boolish(verdicts[1].Passed) != "fail" ||
		!strings.Contains(verdicts[1].Evidence, "no verdict for assertion 2") {
		t.Errorf("verdict[1] = (%s, %q), want loud missing-verdict failure",
			boolish(verdicts[1].Passed), verdicts[1].Evidence)
	}
	if boolish(verdicts[2].Passed) != "pass" || verdicts[2].Evidence != "c" {
		t.Errorf("verdict[2] = (%s, %q), want (pass, c)", boolish(verdicts[2].Passed), verdicts[2].Evidence)
	}
}

// TestJudgeRunsAfterDeterministic pins grading order: the judge session runs
// with full tools, so deterministic assertions (whose command steps may also
// mutate the workspace) must have graded before it — even when the llm
// assertion is authored first.
func TestJudgeRunsAfterDeterministic(t *testing.T) {
	o := opts(t, "x")
	j := o.Judge.(*fakeJudge)
	j.response = `{"verdicts": [{"id": 1, "passed": true, "evidence": "ok"}]}`
	var judgeSawMarker bool
	j.onJudge = func(ws string) {
		_, err := os.Stat(filepath.Join(ws, "marker.txt"))
		judgeSawMarker = err == nil
	}
	verdicts := Case(context.Background(), []evalspec.Assertion{
		{Type: "llm", Text: "authored first, graded last"},
		{Type: "command", Run: "touch marker.txt"},
	}, o)
	if !judgeSawMarker {
		t.Error("judge ran before the command assertion; want deterministic-first grading")
	}
	if boolish(verdicts[0].Passed) != "pass" || boolish(verdicts[1].Passed) != "pass" {
		t.Errorf("verdicts = (%s, %s), want (pass, pass)",
			boolish(verdicts[0].Passed), boolish(verdicts[1].Passed))
	}
}

func TestUnknownAssertionType(t *testing.T) {
	passed, evidence := caseOne(t, evalspec.Assertion{Type: "mystery"}, opts(t, ""))
	if boolish(passed) != "fail" || !strings.Contains(evidence, "unknown assertion type") {
		t.Errorf("unknown type = (%s, %q)", boolish(passed), evidence)
	}
}

func TestLLMJudgeExpectedOutputContext(t *testing.T) {
	o := opts(t, "output")
	j := o.Judge.(*fakeJudge)
	j.response = `{"verdicts": [{"id": 1, "passed": true, "evidence": "ok"}]}`

	// Without expected output the prompt carries no author-context block.
	caseOne(t, evalspec.Assertion{Type: "llm", Text: "t"}, o)
	if strings.Contains(j.gotPrompt, "expected output") {
		t.Errorf("prompt unexpectedly mentions expected output:\n%s", j.gotPrompt)
	}

	o.ExpectedOutput = "a tidy summary table"
	caseOne(t, evalspec.Assertion{Type: "llm", Text: "t"}, o)
	if !strings.Contains(j.gotPrompt, "a tidy summary table") {
		t.Errorf("prompt missing expected-output context:\n%s", j.gotPrompt)
	}
}

func TestToolCallCounting(t *testing.T) {
	ok := `{"status":"ok"}`
	calls := []model.ToolCall{
		{ID: "c1", Name: "confirm_refund", Input: json.RawMessage(`{"orderId":"1234"}`), Result: new(`{"approved":true}`)},
		{ID: "c2", Name: "issue_refund", Input: json.RawMessage(`{"orderId":"1234"}`), Result: &ok},
		{ID: "c3", Name: "issue_refund", Input: json.RawMessage(`{"orderId":"9"}`)},
	}
	tests := []struct {
		name string
		a    evalspec.Assertion
		want string
	}{
		{"default min 1", evalspec.Assertion{Type: "tool_call", Tool: "^issue_refund$"}, "pass"},
		{"max 1 exceeded", evalspec.Assertion{Type: "tool_call", Tool: "^issue_refund$", Max: new(1)}, "fail"},
		{"max 1 with args narrowing", evalspec.Assertion{Type: "tool_call", Tool: "^issue_refund$",
			Pattern: `"1234"`, Max: new(1)}, "pass"},
		{"result match", evalspec.Assertion{Type: "tool_call", Tool: "issue", Result: `"status":\s*"ok"`, Max: new(1)},
			"pass"},
		{"result requires a result", evalspec.Assertion{Type: "tool_call", Tool: "issue", Pattern: `"9"`,
			Result: "."}, "fail"},
		{"never called holds", evalspec.Assertion{Type: "tool_call", Tool: "delete_account", Max: new(0)}, "pass"},
		{"never called broken", evalspec.Assertion{Type: "tool_call", Tool: "confirm_refund", Max: new(0)}, "fail"},
		{"min 2", evalspec.Assertion{Type: "tool_call", Tool: "issue_refund", Min: new(2)}, "pass"},
		{"min 3", evalspec.Assertion{Type: "tool_call", Tool: "issue_refund", Min: new(3)}, "fail"},
		{"min 0 max 0 none", evalspec.Assertion{Type: "tool_call", Tool: "x", Min: new(0), Max: new(0)}, "pass"},
		{"invalid result regex", evalspec.Assertion{Type: "tool_call", Tool: "x", Result: "("}, "fail"},
	}
	for _, tt := range tests {
		o := opts(t, "")
		o.ToolCalls = calls
		passed, evidence := caseOne(t, tt.a, o)
		if boolish(passed) != tt.want {
			t.Errorf("%s: (%s, %q), want %s", tt.name, boolish(passed), evidence, tt.want)
		}
	}
	o := opts(t, "")
	o.ToolCalls = calls
	if _, evidence := caseOne(t, evalspec.Assertion{Type: "tool_call", Tool: "issue_refund", Max: new(1)}, o); evidence !=
		`2 matching calls (exactly 1): issue_refund {"orderId":"1234"}` {
		t.Errorf("evidence = %q", evidence)
	}
}

func TestToolSequence(t *testing.T) {
	calls := []model.ToolCall{{Name: "lookup"}, {Name: "confirm_refund"}, {Name: "log"}, {Name: "issue_refund"}}
	tests := []struct {
		name  string
		tools []string
		calls []model.ToolCall
		want  string
	}{
		{"ordered subsequence", []string{"^confirm_refund$", "^issue_refund$"}, calls, "pass"},
		{"wrong order", []string{"^issue_refund$", "^confirm_refund$"}, calls, "fail"},
		{"repeated pattern needs repeated calls", []string{"refund", "refund", "refund"}, calls, "fail"},
		{"skip without reporter", []string{"x"}, nil, "skip"},
		{"none reported", []string{"x"}, []model.ToolCall{}, "fail"},
	}
	for _, tt := range tests {
		o := opts(t, "")
		o.ToolCalls = tt.calls
		passed, evidence := caseOne(t, evalspec.Assertion{Type: "tool_sequence", Tools: tt.tools}, o)
		if boolish(passed) != tt.want {
			t.Errorf("%s: (%s, %q), want %s", tt.name, boolish(passed), evidence, tt.want)
		}
	}
	o := opts(t, "")
	o.ToolCalls = calls
	if _, evidence := caseOne(t, evalspec.Assertion{Type: "tool_sequence",
		Tools: []string{"confirm", "issue", "email"}}, o); evidence != "matched 2/3; no call to /email/ after issue_refund" {
		t.Errorf("evidence = %q", evidence)
	}
}

func TestStateAssertions(t *testing.T) {
	state := json.RawMessage(`{"refunds":[{"orderId":"1234","amount":20.5}],"note":"Receipt sent","a/b":true}`)
	tests := []struct {
		name  string
		a     evalspec.Assertion
		state json.RawMessage
		want  string
	}{
		{"exists", evalspec.Assertion{Type: "state", Pointer: "/refunds/0"}, state, "pass"},
		{"absent", evalspec.Assertion{Type: "state", Pointer: "/refunds/1"}, state, "fail"},
		{"equals string", evalspec.Assertion{Type: "state", Pointer: "/refunds/0/orderId",
			Equals: json.RawMessage(`"1234"`)}, state, "pass"},
		{"equals number", evalspec.Assertion{Type: "state", Pointer: "/refunds/0/amount",
			Equals: json.RawMessage(`20.50`)}, state, "pass"},
		{"equals object", evalspec.Assertion{Type: "state", Pointer: "/refunds/0",
			Equals: json.RawMessage(`{"amount":20.5,"orderId":"1234"}`)}, state, "pass"},
		{"equals mismatch", evalspec.Assertion{Type: "state", Pointer: "/refunds/0/orderId",
			Equals: json.RawMessage(`1234`)}, state, "fail"},
		{"pattern on string", evalspec.Assertion{Type: "state", Pointer: "/note", Pattern: "(?i)receipt"}, state, "pass"},
		{"pattern on object", evalspec.Assertion{Type: "state", Pointer: "/refunds", Pattern: `"orderId":"1234"`},
			state, "pass"},
		{"escaped pointer", evalspec.Assertion{Type: "state", Pointer: "/a~1b", Equals: json.RawMessage(`true`)},
			state, "pass"},
		{"whole state", evalspec.Assertion{Type: "state", Pattern: "Receipt"}, state, "pass"},
		{"skip without state", evalspec.Assertion{Type: "state", Pointer: "/x"}, nil, "skip"},
		{"bad pointer", evalspec.Assertion{Type: "state", Pointer: "x"}, state, "fail"},
	}
	for _, tt := range tests {
		o := opts(t, "")
		o.State = tt.state
		passed, evidence := caseOne(t, tt.a, o)
		if boolish(passed) != tt.want {
			t.Errorf("%s: (%s, %q), want %s", tt.name, boolish(passed), evidence, tt.want)
		}
	}
}

func TestHITLAssertions(t *testing.T) {
	reqs := []model.HITLRequest{
		{Kind: "tool", Name: "confirm_refund", Detail: `{"orderId":"1234"}`, Answered: true},
		{Kind: "interrupt", Reason: "confirmation", Name: "send_email", Detail: "Send the receipt?", Answered: true},
	}
	tests := []struct {
		name string
		a    evalspec.Assertion
		reqs []model.HITLRequest
		want string
	}{
		{"any request", evalspec.Assertion{Type: "hitl"}, reqs, "pass"},
		{"by tool", evalspec.Assertion{Type: "hitl", Tool: "^confirm_refund$"}, reqs, "pass"},
		{"interrupt linked tool", evalspec.Assertion{Type: "hitl", Tool: "send_email"}, reqs, "pass"},
		{"by reason", evalspec.Assertion{Type: "hitl", Reason: "^confirmation$"}, reqs, "pass"},
		{"reason only counts interrupts", evalspec.Assertion{Type: "hitl", Reason: ".*", Max: new(1)}, reqs, "pass"},
		{"by pattern", evalspec.Assertion{Type: "hitl", Pattern: "(?i)receipt"}, reqs, "pass"},
		{"never asked broken", evalspec.Assertion{Type: "hitl", Max: new(0)}, reqs, "fail"},
		{"never asked holds", evalspec.Assertion{Type: "hitl", Max: new(0)}, []model.HITLRequest{}, "pass"},
		{"too few", evalspec.Assertion{Type: "hitl", Min: new(3)}, reqs, "fail"},
		{"skip outside a conversation", evalspec.Assertion{Type: "hitl"}, nil, "skip"},
	}
	for _, tt := range tests {
		o := opts(t, "")
		o.HITL = tt.reqs
		passed, evidence := caseOne(t, tt.a, o)
		if boolish(passed) != tt.want {
			t.Errorf("%s: (%s, %q), want %s", tt.name, boolish(passed), evidence, tt.want)
		}
	}
}

func TestOutcomeAndConformance(t *testing.T) {
	o := opts(t, "")
	if passed, _ := caseOne(t, evalspec.Assertion{Type: "outcome", Expect: "completed"}, o); passed != nil {
		t.Error("outcome must skip outside a conversation")
	}
	if passed, _ := caseOne(t, evalspec.Assertion{Type: "conformance"}, o); passed != nil {
		t.Error("conformance must skip outside a conversation")
	}
	o.Outcome = "awaiting_input"
	if passed, ev := caseOne(t, evalspec.Assertion{Type: "outcome", Expect: "completed"}, o); boolish(passed) != "fail" ||
		ev != "conversation ended awaiting_input" {
		t.Errorf("outcome = %s %q", boolish(passed), ev)
	}
	if passed, _ := caseOne(t, evalspec.Assertion{Type: "outcome", Expect: "awaiting_input"}, o); boolish(passed) != "pass" {
		t.Error("matching outcome must pass")
	}
	o.Violations = []string{}
	if passed, _ := caseOne(t, evalspec.Assertion{Type: "conformance"}, o); boolish(passed) != "pass" {
		t.Error("no violations must pass")
	}
	o.Violations = []string{"pairing: message m1 still open at RUN_FINISHED"}
	if passed, ev := caseOne(t, evalspec.Assertion{Type: "conformance"}, o); boolish(passed) != "fail" ||
		!strings.Contains(ev, "1 violation: pairing") {
		t.Errorf("conformance = %s %q", boolish(passed), ev)
	}
}

func TestJudgeContext(t *testing.T) {
	o := opts(t, "out")
	j := o.Judge.(*fakeJudge)
	j.response = `{"verdicts":[{"id":1,"passed":true,"evidence":"e"}]}`
	o.JudgeContext = "The agent ran remotely; its transcript is at .agent/transcript.md."
	caseOne(t, evalspec.Assertion{Type: "llm", Text: "t"}, o)
	if !strings.Contains(j.gotPrompt, "verify the assertions.\n\nThe agent ran remotely; its transcript is at "+
		".agent/transcript.md.\n\nGrade every assertion") {
		t.Errorf("prompt = %s", j.gotPrompt)
	}
	o.JudgeContext = ""
	caseOne(t, evalspec.Assertion{Type: "llm", Text: "t"}, o)
	if !strings.Contains(j.gotPrompt, "verify the assertions.\n\nGrade every assertion") {
		t.Errorf("prompt without context = %s", j.gotPrompt)
	}
}

func TestDescribeNewTypes(t *testing.T) {
	tests := []struct {
		a    evalspec.Assertion
		want string
	}{
		{evalspec.Assertion{Type: "tool_call", Tool: "Write"}, "agent called tool /Write/"},
		{evalspec.Assertion{Type: "tool_call", Tool: "Bash", Pattern: "go"}, "agent called tool /Bash/ with args /go/"},
		{evalspec.Assertion{Type: "tool_call", Tool: "issue", Result: "ok", Max: new(1)},
			"agent called tool /issue/ returning /ok/ (exactly 1 time)"},
		{evalspec.Assertion{Type: "tool_call", Tool: "rm", Max: new(0)}, "agent never called tool /rm/"},
		{evalspec.Assertion{Type: "tool_call", Tool: "t", Min: new(2)}, "agent called tool /t/ (at least 2 times)"},
		{evalspec.Assertion{Type: "tool_sequence", Tools: []string{"a", "b"}}, "agent called tools in order: /a/ → /b/"},
		{evalspec.Assertion{Type: "state", Pointer: "/x"}, "state/x exists"},
		{evalspec.Assertion{Type: "state", Pointer: "/x", Equals: json.RawMessage(`{ "a": 1 }`)}, `state/x equals {"a":1}`},
		{evalspec.Assertion{Type: "state", Pointer: "/x", Pattern: "y"}, "state/x matches /y/"},
		{evalspec.Assertion{Type: "hitl", Tool: "confirm", Max: new(1)},
			"agent asked for human input via tool /confirm/ (exactly 1 request)"},
		{evalspec.Assertion{Type: "hitl", Reason: "confirmation"}, "agent asked for human input with reason /confirmation/"},
		{evalspec.Assertion{Type: "outcome", Expect: "completed"}, "conversation ended completed"},
		{evalspec.Assertion{Type: "conformance"}, "agent's event stream conforms to the protocol"},
	}
	for _, tt := range tests {
		if got := Describe(tt.a); got != tt.want {
			t.Errorf("Describe(%+v) = %q, want %q", tt.a, got, tt.want)
		}
	}
}
