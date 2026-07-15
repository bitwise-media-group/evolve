// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package grade

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/runner"
)

// judgeRunner fakes the claude CLI; everything else runs for real.
type judgeRunner struct {
	exec     runner.Exec
	response string
	gotSpec  model.CommandSpec
}

func (j *judgeRunner) Run(ctx context.Context, spec model.CommandSpec, timeout time.Duration, scan *runner.Scan) (runner.Result, error) {
	if spec.Argv[0] == "claude" {
		j.gotSpec = spec
		return runner.Result{Stdout: []byte(j.response)}, nil
	}
	return j.exec.Run(ctx, spec, timeout, scan)
}

func opts(t *testing.T, output string) Options {
	t.Helper()
	return Options{
		Runner:    &judgeRunner{},
		Workspace: t.TempDir(),
		Output:    output,
		Timeout:   10 * time.Second,
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
	j := o.Runner.(*judgeRunner)
	j.response = `{"result": "Sure! Here is the verdict:\n{\"passed\": true, \"evidence\": \"README covers omissions\"}"}`

	passed, evidence := Assertion(context.Background(), evalspec.Assertion{Type: "llm", Text: "README explains omissions"}, o)
	if boolish(passed) != "pass" || evidence != "README covers omissions" {
		t.Errorf("judge = (%s, %q)", boolish(passed), evidence)
	}
	// The judge is pinned to a model and read-only tools.
	cmdline := strings.Join(j.gotSpec.Argv, " ")
	if !strings.Contains(cmdline, "--model "+DefaultJudgeModel) || !strings.Contains(cmdline, "Read Glob Grep") {
		t.Errorf("judge invocation = %q", cmdline)
	}
}

func TestLLMJudgeErrorsFailLoudly(t *testing.T) {
	o := opts(t, "x")
	o.Runner.(*judgeRunner).response = `total garbage`
	passed, evidence := Assertion(context.Background(), evalspec.Assertion{Type: "llm", Text: "anything"}, o)
	if boolish(passed) != "fail" || !strings.Contains(evidence, "judge error") {
		t.Errorf("judge garbage = (%s, %q), want loud failure", boolish(passed), evidence)
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
	j := o.Runner.(*judgeRunner)
	j.response = `{"result": "{\"passed\": true, \"evidence\": \"ok\"}"}`

	// Without expected output the prompt carries no author-context block.
	Assertion(context.Background(), evalspec.Assertion{Type: "llm", Text: "t"}, o)
	if strings.Contains(j.gotSpec.Argv[2], "expected output") {
		t.Errorf("prompt unexpectedly mentions expected output:\n%s", j.gotSpec.Argv[2])
	}

	o.ExpectedOutput = "a tidy summary table"
	Assertion(context.Background(), evalspec.Assertion{Type: "llm", Text: "t"}, o)
	if !strings.Contains(j.gotSpec.Argv[2], "a tidy summary table") {
		t.Errorf("prompt missing expected-output context:\n%s", j.gotSpec.Argv[2])
	}
}
