// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package evalspec

import (
	"fmt"
	"slices"

	"github.com/bitwise-media-group/evolve/internal/encfmt"
)

// Trigger is one trigger-accuracy query.
type Trigger struct {
	Query         string   `json:"query"`
	ShouldTrigger bool     `json:"should_trigger"`
	SkipProviders []string `json:"skip_providers,omitempty"`
}

// TriggersFile is one authored triggers document: the same envelope shape as
// skill-creator's evals.json, {skill_name?, triggers: [...]}.
type TriggersFile struct {
	SkillName string    `json:"skill_name,omitempty"`
	Triggers  []Trigger `json:"triggers"`
}

// SkipsProvider reports whether the trigger opts out of a provider.
func (t Trigger) SkipsProvider(name string) bool { return slices.Contains(t.SkipProviders, name) }

// LoadTriggers parses an authored triggers file in any supported format.
func LoadTriggers(path string) (*TriggersFile, error) {
	var f TriggersFile
	if err := encfmt.DecodeFile(path, &f); err != nil {
		return nil, err
	}
	return &f, nil
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
