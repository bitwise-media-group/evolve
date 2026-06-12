// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package evalspec

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
)

// Trigger is one trigger-accuracy query.
type Trigger struct {
	Query         string   `json:"query"`
	ShouldTrigger bool     `json:"should_trigger"`
	SkipProviders []string `json:"skip_providers,omitempty"`
}

// Assertion is one graded condition of a behavioral case.
type Assertion struct {
	Type       string `json:"type"`
	Path       string `json:"path,omitempty"`
	Pattern    string `json:"pattern,omitempty"`
	Run        string `json:"run,omitempty"`
	Cwd        string `json:"cwd,omitempty"`
	Requires   string `json:"requires,omitempty"`
	ExpectExit *int   `json:"expect_exit,omitempty"`
	Text       string `json:"text,omitempty"`
}

// AssertionTypes is the closed set of supported assertion kinds.
var AssertionTypes = []string{"file_exists", "file_absent", "regex", "not_regex", "command", "llm"}

// Case is one behavioral case.
type Case struct {
	ID             string            `json:"id"`
	Prompt         string            `json:"prompt"`
	Files          map[string]string `json:"files,omitempty"`
	MaxTurns       int               `json:"max_turns,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	AllowedTools   string            `json:"allowed_tools,omitempty"`
	SkipProviders  []string          `json:"skip_providers,omitempty"`
	Assertions     []Assertion       `json:"assertions"`
}

// SkipsProvider reports whether the trigger opts out of a provider.
func (t Trigger) SkipsProvider(name string) bool { return slices.Contains(t.SkipProviders, name) }

// SkipsProvider reports whether the case opts out of a provider.
func (c Case) SkipsProvider(name string) bool { return slices.Contains(c.SkipProviders, name) }

// LoadTriggers parses a triggers.json.
func LoadTriggers(path string) ([]Trigger, error) {
	var triggers []Trigger
	if err := loadJSON(path, &triggers); err != nil {
		return nil, err
	}
	return triggers, nil
}

// LoadCases parses a cases.json.
func LoadCases(path string) ([]Case, error) {
	var cases []Case
	if err := loadJSON(path, &cases); err != nil {
		return nil, err
	}
	return cases, nil
}

func loadJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// ValidateTriggers returns the problems in an authored trigger list.
func ValidateTriggers(triggers []Trigger) []string {
	var problems []string
	seen := map[string]bool{}
	for i, t := range triggers {
		switch {
		case t.Query == "":
			problems = append(problems, fmt.Sprintf("triggers[%d]: empty query", i))
		case seen[t.Query]:
			problems = append(problems, fmt.Sprintf("triggers[%d]: duplicate query %q", i, t.Query))
		}
		seen[t.Query] = true
	}
	return problems
}

// ValidateCases returns the problems in an authored case list.
func ValidateCases(cases []Case) []string {
	var problems []string
	seen := map[string]bool{}
	for i, c := range cases {
		label := fmt.Sprintf("cases[%d]", i)
		if c.ID != "" {
			label = fmt.Sprintf("case %q", c.ID)
		}
		switch {
		case c.ID == "":
			problems = append(problems, fmt.Sprintf("cases[%d]: missing id", i))
		case seen[c.ID]:
			problems = append(problems, fmt.Sprintf("%s: duplicate id", label))
		}
		seen[c.ID] = true
		if c.Prompt == "" {
			problems = append(problems, label+": missing prompt")
		}
		if len(c.Assertions) == 0 {
			problems = append(problems, label+": no assertions")
		}
		for j, a := range c.Assertions {
			problems = append(problems, validateAssertion(a, fmt.Sprintf("%s assertions[%d]", label, j))...)
		}
	}
	return problems
}

func validateAssertion(a Assertion, label string) []string {
	var problems []string
	switch a.Type {
	case "file_exists", "file_absent":
		if a.Path == "" {
			problems = append(problems, label+": missing path")
		}
	case "regex", "not_regex":
		if a.Pattern == "" {
			problems = append(problems, label+": missing pattern")
		} else if _, err := regexp.Compile("(?m)" + a.Pattern); err != nil {
			problems = append(problems, fmt.Sprintf("%s: invalid pattern: %v", label, err))
		}
	case "command":
		if a.Run == "" {
			problems = append(problems, label+": missing run")
		}
	case "llm":
		if a.Text == "" {
			problems = append(problems, label+": missing text")
		}
	default:
		problems = append(problems, fmt.Sprintf("%s: unknown assertion type %q", label, a.Type))
	}
	return problems
}
