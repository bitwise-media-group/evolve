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
