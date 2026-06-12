// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package evalspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadTriggersCompat(t *testing.T) {
	// Byte-compatible with the Python harness's authored format.
	path := write(t, `[
		{"query": "Write tests for this Go package", "should_trigger": true},
		{"query": "Write pytest tests", "should_trigger": false, "skip_providers": ["cursor"]}
	]`)
	triggers, err := LoadTriggers(path)
	if err != nil {
		t.Fatal(err)
	}
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

func TestLoadCasesCompat(t *testing.T) {
	path := write(t, `[{
		"id": "table-driven",
		"prompt": "Write tests for Clamp",
		"allowed_tools": "Read Write Edit Bash(go *)",
		"files": {"go.mod": "module example.com/x\n"},
		"max_turns": 30,
		"assertions": [
			{"type": "file_exists", "path": "clamp_test.go"},
			{"type": "regex", "path": "clamp_test.go", "pattern": "t\\.Run\\("},
			{"type": "not_regex", "pattern": "testify"},
			{"type": "command", "run": "go test ./...", "requires": "go", "expect_exit": 0},
			{"type": "llm", "text": "Tests cover both bounds"}
		]
	}]`)
	cases, err := LoadCases(path)
	if err != nil {
		t.Fatal(err)
	}
	c := cases[0]
	if c.ID != "table-driven" || c.MaxTurns != 30 || c.Files["go.mod"] == "" || len(c.Assertions) != 5 {
		t.Errorf("case = %+v", c)
	}
	if c.Assertions[3].ExpectExit == nil || *c.Assertions[3].ExpectExit != 0 {
		t.Errorf("expect_exit = %v", c.Assertions[3].ExpectExit)
	}
	if problems := ValidateCases(cases); len(problems) != 0 {
		t.Errorf("problems = %v", problems)
	}
}

func TestValidateCatchesProblems(t *testing.T) {
	cases := []Case{
		{ID: "a", Prompt: "p", Assertions: []Assertion{{Type: "regexp", Pattern: "x"}}},
		{ID: "a", Prompt: "", Assertions: []Assertion{{Type: "regex", Pattern: "("}}},
		{Prompt: "p"},
	}
	problems := strings.Join(ValidateCases(cases), "\n")
	for _, want := range []string{
		`unknown assertion type "regexp"`,
		"duplicate id",
		"missing prompt",
		"invalid pattern",
		"missing id",
		"no assertions",
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
