// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package evalspec

import (
	"strings"
	"testing"
)

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

func TestValidateTriggersCatchesProblems(t *testing.T) {
	triggers := []Trigger{{Query: ""}, {Query: "q"}, {Query: "q"}}
	tp := strings.Join(ValidateTriggers(triggers), "\n")
	if !strings.Contains(tp, "empty query") || !strings.Contains(tp, "duplicate query") {
		t.Errorf("trigger problems = %s", tp)
	}
}
