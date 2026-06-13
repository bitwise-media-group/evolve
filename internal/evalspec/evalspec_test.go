// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package evalspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, rel, content string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadTriggers(t *testing.T) {
	path := write(t, t.TempDir(), "triggers.json", `{
		"skill_name": "go-tests",
		"triggers": [
			{"query": "Write tests for this Go package", "should_trigger": true},
			{"query": "Write pytest tests", "should_trigger": false, "skip_providers": ["cursor"]}
		]
	}`)
	spec, err := LoadTriggers(path)
	if err != nil {
		t.Fatal(err)
	}
	if spec.SkillName != "go-tests" {
		t.Errorf("skill_name = %q", spec.SkillName)
	}
	triggers := spec.Triggers
	if len(triggers) != 2 || !triggers[0].ShouldTrigger || triggers[1].ShouldTrigger {
		t.Errorf("triggers = %+v", triggers)
	}
	if !triggers[1].SkipsProvider("cursor") || triggers[0].SkipsProvider("cursor") {
		t.Error("skip_providers not honored")
	}
	if problems := ValidateTriggers(triggers); len(problems) != 0 {
		t.Errorf("problems = %v", problems)
	}
}

func TestLoadEvalsEvolveStyle(t *testing.T) {
	path := write(t, t.TempDir(), "evals.json", `{"evals": [{
		"id": "table-driven",
		"prompt": "Write tests for Clamp",
		"allowed_tools": "Read Write Edit Bash(go *)",
		"max_turns": 30,
		"assertions": [
			{"type": "file_exists", "path": "clamp_test.go"},
			{"type": "regex", "path": "clamp_test.go", "pattern": "t\\.Run\\("},
			{"type": "not_regex", "pattern": "testify"},
			{"type": "command", "run": "go test ./...", "requires": "go", "expect_exit": 0},
			{"type": "llm", "text": "Tests cover both bounds"}
		]
	}]}`)
	spec, err := LoadEvals(path)
	if err != nil {
		t.Fatal(err)
	}
	c := spec.Evals[0]
	if c.ID != "table-driven" || c.MaxTurns != 30 || len(c.Assertions) != 5 {
		t.Errorf("eval = %+v", c)
	}
	if c.Assertions[3].ExpectExit == nil || *c.Assertions[3].ExpectExit != 0 {
		t.Errorf("expect_exit = %v", c.Assertions[3].ExpectExit)
	}
	if problems := ValidateEvals(spec.Evals); len(problems) != 0 {
		t.Errorf("problems = %v", problems)
	}
}

// TestLoadEvalsSkillCreatorDropIn pins the migration contract: a verbatim
// skill-creator evals.json (integer ids, path-list files, string
// expectations) loads without edits.
func TestLoadEvalsSkillCreatorDropIn(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "files/sample1.pdf", "%PDF-1.4 fake")
	path := write(t, dir, "evals.json", `{
		"skill_name": "example-skill",
		"evals": [
			{
				"id": 1,
				"prompt": "User's example prompt",
				"expected_output": "Description of expected result",
				"files": ["evals/files/sample1.pdf"],
				"expectations": [
					"The output includes X",
					"The skill used script Y"
				]
			}
		]
	}`)
	spec, err := LoadEvals(path)
	if err != nil {
		t.Fatal(err)
	}
	c := spec.Evals[0]
	if c.ID != "1" {
		t.Errorf("integer id = %q, want normalized \"1\"", c.ID)
	}
	if c.ExpectedOutput != "Description of expected result" {
		t.Errorf("expected_output = %q", c.ExpectedOutput)
	}
	// The skill-root-relative evals/ prefix resolves against the eval dir,
	// and a files/ fixture stages by its path under files/.
	ref := c.Files[0]
	if ref.Source != filepath.Join(dir, "files", "sample1.pdf") || ref.Dest != "sample1.pdf" {
		t.Errorf("ref = %+v", ref)
	}
	// Expectations expand to llm assertions, graded in authored order.
	if len(c.Assertions) != 2 || c.Assertions[0].Type != "llm" ||
		c.Assertions[0].Text != "The output includes X" || !c.Assertions[0].FromExpectation {
		t.Errorf("assertions = %+v", c.Assertions)
	}
	if problems := ValidateEvals(spec.Evals); len(problems) != 0 {
		t.Errorf("problems = %v", problems)
	}
}

func TestLoadEvalsExpectationsPrecedeAssertions(t *testing.T) {
	path := write(t, t.TempDir(), "evals.json", `{"evals": [{
		"id": "mixed",
		"prompt": "p",
		"expectations": ["stated first"],
		"assertions": ["bare string is llm sugar", {"type": "file_exists", "path": "out.txt"}]
	}]}`)
	spec, err := LoadEvals(path)
	if err != nil {
		t.Fatal(err)
	}
	a := spec.Evals[0].Assertions
	if len(a) != 3 {
		t.Fatalf("assertions = %+v", a)
	}
	if a[0].Text != "stated first" || !a[0].FromExpectation {
		t.Errorf("a[0] = %+v, want the expectation graded first", a[0])
	}
	if a[1].Type != "llm" || a[1].Text != "bare string is llm sugar" || a[1].FromExpectation {
		t.Errorf("a[1] = %+v, want llm sugar from the bare string", a[1])
	}
	if a[2].Type != "file_exists" {
		t.Errorf("a[2] = %+v", a[2])
	}
}

func TestLoadEvalsFileStaging(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "files/src/main.go", "package main")
	write(t, dir, "seed.txt", "seed")
	path := write(t, dir, "evals.json", `{"evals": [{
		"id": "staging",
		"prompt": "p",
		"files": ["files/src/main.go", "seed.txt"],
		"expectations": ["x"]
	}]}`)
	spec, err := LoadEvals(path)
	if err != nil {
		t.Fatal(err)
	}
	refs := spec.Evals[0].Files
	if refs[0].Dest != filepath.Join("src", "main.go") {
		t.Errorf("files/ path dest = %q, want nested path preserved", refs[0].Dest)
	}
	if refs[1].Dest != "seed.txt" || refs[1].Source != filepath.Join(dir, "seed.txt") {
		t.Errorf("bare path ref = %+v, want basename staging", refs[1])
	}
	if problems := ValidateEvals(spec.Evals); len(problems) != 0 {
		t.Errorf("problems = %v", problems)
	}
}

func TestLoadEvalsRejectsInlineFiles(t *testing.T) {
	path := write(t, t.TempDir(), "evals.json",
		`{"evals": [{"id": "x", "prompt": "p", "files": {"go.mod": "module x"}, "expectations": ["y"]}]}`)
	_, err := LoadEvals(path)
	if err == nil || !strings.Contains(err.Error(), "inline file content is not supported") {
		t.Errorf("err = %v, want pointed inline-files error", err)
	}
}

func TestLoadEvalsRejectsEscapingFixture(t *testing.T) {
	path := write(t, t.TempDir(), "evals.json",
		`{"evals": [{"id": "x", "prompt": "p", "files": ["../secrets.txt"], "expectations": ["y"]}]}`)
	_, err := LoadEvals(path)
	if err == nil || !strings.Contains(err.Error(), "escapes the evals directory") {
		t.Errorf("err = %v, want escape error", err)
	}
}

func TestLoadEvalsRejectsBadID(t *testing.T) {
	path := write(t, t.TempDir(), "evals.json",
		`{"evals": [{"id": true, "prompt": "p", "expectations": ["y"]}]}`)
	if _, err := LoadEvals(path); err == nil || !strings.Contains(err.Error(), "string or an integer") {
		t.Errorf("err = %v, want id type error", err)
	}
}

func TestValidateCatchesProblems(t *testing.T) {
	evals := []Eval{
		{ID: "a", Prompt: "p", Assertions: []Assertion{{Type: "regexp", Pattern: "x"}}},
		{ID: "a", Prompt: "", Assertions: []Assertion{{Type: "regex", Pattern: "("}}},
		{Prompt: "p"},
		{ID: "f", Prompt: "p", Assertions: []Assertion{{Type: "llm", Text: "t"}},
			Files: []FileRef{
				{Rel: "files/a.txt", Source: "/no/such/fixture", Dest: "a.txt"},
				{Rel: "a.txt", Source: "/no/such/either", Dest: "a.txt"},
			}},
	}
	problems := strings.Join(ValidateEvals(evals), "\n")
	for _, want := range []string{
		`unknown assertion type "regexp"`,
		"duplicate id",
		"missing prompt",
		"invalid pattern",
		"missing id",
		"no expectations or assertions",
		"fixture not found",
		"both stage to a.txt",
	} {
		if !strings.Contains(problems, want) {
			t.Errorf("problems missing %q:\n%s", want, problems)
		}
	}

	triggers := []Trigger{{Query: ""}, {Query: "q"}, {Query: "q"}}
	tp := strings.Join(ValidateTriggers(triggers), "\n")
	if !strings.Contains(tp, "empty query") || !strings.Contains(tp, "duplicate query") {
		t.Errorf("trigger problems = %s", tp)
	}
}

// TestIntegerAndStringIDsCollide pins the normalization rule: 1 and "1" are
// the same id after loading.
func TestIntegerAndStringIDsCollide(t *testing.T) {
	path := write(t, t.TempDir(), "evals.json",
		`{"evals": [
			{"id": 1, "prompt": "p", "expectations": ["x"]},
			{"id": "1", "prompt": "p", "expectations": ["x"]}
		]}`)
	spec, err := LoadEvals(path)
	if err != nil {
		t.Fatal(err)
	}
	if problems := strings.Join(ValidateEvals(spec.Evals), "\n"); !strings.Contains(problems, "duplicate id") {
		t.Errorf("problems = %s, want duplicate id", problems)
	}
}
