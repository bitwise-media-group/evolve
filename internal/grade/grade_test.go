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
type fakeJudge struct {
	response  string
	err       error
	gotWS     string
	gotPrompt string
}

func (f *fakeJudge) Judge(_ context.Context, ws, prompt string, _ time.Duration) (string, error) {
	f.gotWS, f.gotPrompt = ws, prompt
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
		passed, _ := Assertion(context.Background(), tt.a, o)
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
		passed, evidence := Assertion(context.Background(), tt.a, o)
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
		passed, evidence := Assertion(context.Background(), tt.a, o)
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
		passed, evidence := Assertion(context.Background(), tt.a, o)
		if boolish(passed) != tt.want {
			t.Errorf("%s: %+v = (%s, %q), want %s", tt.name, tt.a, boolish(passed), evidence, tt.want)
		}
	}
}

func TestLLMJudge(t *testing.T) {
	o := opts(t, "the readme explains tradeoffs")
	j := o.Judge.(*fakeJudge)
	// The verdict blob is extracted from surrounding prose, whatever harness
	// produced the text.
	j.response = "Sure! Here is the verdict:\n{\"passed\": true, \"evidence\": \"README covers omissions\"}"

	passed, evidence := Assertion(context.Background(), evalspec.Assertion{Type: "llm", Text: "README explains omissions"}, o)
	if boolish(passed) != "pass" || evidence != "README covers omissions" {
		t.Errorf("judge = (%s, %q)", boolish(passed), evidence)
	}
	// The judge session runs in the eval's workspace and is asked exactly the
	// authored assertion.
	if j.gotWS != o.Workspace {
		t.Errorf("judge workspace = %q, want %q", j.gotWS, o.Workspace)
	}
	if !strings.Contains(j.gotPrompt, "README explains omissions") {
		t.Errorf("judge prompt missing assertion text:\n%s", j.gotPrompt)
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
		tt.set(&o)
		passed, evidence := Assertion(context.Background(), evalspec.Assertion{Type: "llm", Text: "anything"}, o)
		if boolish(passed) != "fail" || !strings.Contains(evidence, "judge error") {
			t.Errorf("%s: judge = (%s, %q), want loud failure", tt.name, boolish(passed), evidence)
		}
	}
}

func TestUnknownAssertionType(t *testing.T) {
	passed, evidence := Assertion(context.Background(), evalspec.Assertion{Type: "mystery"}, opts(t, ""))
	if boolish(passed) != "fail" || !strings.Contains(evidence, "unknown assertion type") {
		t.Errorf("unknown type = (%s, %q)", boolish(passed), evidence)
	}
}

func TestLLMJudgeExpectedOutputContext(t *testing.T) {
	o := opts(t, "output")
	j := o.Judge.(*fakeJudge)
	j.response = `{"passed": true, "evidence": "ok"}`

	// Without expected output the prompt carries no author-context block.
	Assertion(context.Background(), evalspec.Assertion{Type: "llm", Text: "t"}, o)
	if strings.Contains(j.gotPrompt, "expected output") {
		t.Errorf("prompt unexpectedly mentions expected output:\n%s", j.gotPrompt)
	}

	o.ExpectedOutput = "a tidy summary table"
	Assertion(context.Background(), evalspec.Assertion{Type: "llm", Text: "t"}, o)
	if !strings.Contains(j.gotPrompt, "a tidy summary table") {
		t.Errorf("prompt missing expected-output context:\n%s", j.gotPrompt)
	}
}
