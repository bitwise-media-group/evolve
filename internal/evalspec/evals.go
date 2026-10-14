// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package evalspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	slashpath "path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/agui"
	"github.com/bitwise-media-group/evolve/internal/encfmt"
)

// Assertion is one graded condition of a behavioral eval. In the authored
// assertions array a bare string is shorthand for {type: "llm", text: ...}.
// Every field beyond the original set is omitempty, so an eval that does not
// use them fingerprints exactly as before.
type Assertion struct {
	Type    string `json:"type"`
	Path    string `json:"path,omitempty"`
	Pattern string `json:"pattern,omitempty"`
	Run     string `json:"run,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	// Tool is a regex matched against an observed tool name for a tool_call
	// assertion (an MCP tool surfaces as "mcp__<server>__<tool>"); Pattern, when
	// set, is matched against the call's JSON-serialized arguments. For hitl it
	// matches the requested tool's name.
	Tool       string `json:"tool,omitempty"`
	Requires   string `json:"requires,omitempty"`
	ExpectExit *int   `json:"expect_exit,omitempty"`
	Text       string `json:"text,omitempty"`

	// Min and Max bound a tool_call or hitl match count (tool_call's min
	// defaults to 1, or 0 when max is 0: "never called").
	Min *int `json:"min,omitempty"`
	Max *int `json:"max,omitempty"`
	// Result is a regex a tool_call's result must match.
	Result string `json:"result,omitempty"`
	// Tools is a tool_sequence's ordered tool-name regexes.
	Tools []string `json:"tools,omitempty"`
	// Pointer (RFC 6901) addresses the final shared state for a state
	// assertion; Equals, when set, is the JSON value it must equal.
	Pointer string          `json:"pointer,omitempty"`
	Equals  json.RawMessage `json:"equals,omitempty"`
	// Reason is a regex on an interrupt's reason for a hitl assertion.
	Reason string `json:"reason,omitempty"`
	// Expect is the conversation outcome an outcome assertion requires.
	Expect string `json:"expect,omitempty"`

	// FromExpectation marks an assertion expanded from the eval's
	// expectations list; results echo it as source: "expectation".
	FromExpectation bool `json:"-"`
}

// AssertionTypes is the closed set of supported assertion kinds. state,
// hitl, outcome, and conformance grade a hosted agent's conversation and are
// valid only in agent suites.
var AssertionTypes = []string{
	"file_exists", "file_absent", "regex", "not_regex", "command", "tool_call", "tool_sequence",
	"state", "hitl", "outcome", "conformance", "llm",
}

// FileRef is one input fixture staged into the eval workspace. It is
// authored as a path relative to the evals file's directory; a leading
// "evals/" segment is tolerated so a skill-creator evals/ directory (whose
// paths are skill-root-relative) drops in verbatim. A path under files/
// stages at its path relative to files/; anything else stages by basename.
type FileRef struct {
	Rel    string // authored path, verbatim
	Source string // absolute on-disk fixture path, resolved at load
	Dest   string // workspace-relative destination per the staging rule
}

// UnmarshalJSON decodes an authored files entry, which must be a path.
func (f *FileRef) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &f.Rel); err != nil {
		return fmt.Errorf("must be a fixture path (a string)")
	}
	return nil
}

// MarshalJSON renders the ref back to its authored path form.
func (f FileRef) MarshalJSON() ([]byte, error) { return json.Marshal(f.Rel) }

// Eval is one behavioral eval — a superset of skill-creator's eval object
// (id, prompt, expected_output, files, expectations) plus evolve's
// deterministic assertions and run controls. The agent-suite fields
// (responses, followups, and the protocol-specific tools, state, context, and
// forwarded_props) are rejected in skill suites; files still stage into the
// local workspace (for command assertions and the judge) and are not sent to
// a hosted agent. For an agent, max_turns caps the conversation's runs and
// timeout_seconds bounds the whole conversation.
type Eval struct {
	ID             string      `json:"id"`
	Name           string      `json:"name,omitempty"`
	Prompt         string      `json:"prompt"`
	ExpectedOutput string      `json:"expected_output,omitempty"`
	Files          []FileRef   `json:"files,omitempty"`
	Expectations   []string    `json:"expectations,omitempty"`
	Assertions     []Assertion `json:"assertions,omitempty"`
	MaxTurns       int         `json:"max_turns,omitempty"`
	TimeoutSeconds int         `json:"timeout_seconds,omitempty"`

	Tools          []Tool          `json:"tools,omitempty"`
	State          json.RawMessage `json:"state,omitempty"`
	Context        []ContextEntry  `json:"context,omitempty"`
	ForwardedProps json.RawMessage `json:"forwarded_props,omitempty"`
	Responses      []Response      `json:"responses,omitempty"`
	FollowUps      []string        `json:"followups,omitempty"`
}

// UnmarshalJSON resolves the superset unions: id as a string or an integer
// (normalized to its decimal string), files as fixture paths (the retired
// inline content map gets a pointed error), and assertions entries as
// objects or bare llm strings.
func (e *Eval) UnmarshalJSON(data []byte) error {
	type plain Eval
	aux := struct {
		*plain
		ID         json.RawMessage   `json:"id"`
		Files      json.RawMessage   `json:"files"`
		Assertions []json.RawMessage `json:"assertions"`
	}{plain: (*plain)(e)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if raw := bytes.TrimSpace(aux.ID); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		var n int64
		switch {
		case json.Unmarshal(raw, &e.ID) == nil:
		case json.Unmarshal(raw, &n) == nil:
			e.ID = strconv.FormatInt(n, 10)
		default:
			return fmt.Errorf("id %s: must be a string or an integer", raw)
		}
	}

	if raw := bytes.TrimSpace(aux.Files); len(raw) > 0 && raw[0] == '{' {
		return fmt.Errorf("eval %q: files: inline file content is not supported; "+
			"list fixture paths relative to the evals file instead", e.ID)
	} else if len(raw) > 0 {
		if err := json.Unmarshal(raw, &e.Files); err != nil {
			return fmt.Errorf("eval %q: files: %w", e.ID, err)
		}
	}

	for i, raw := range aux.Assertions {
		var a Assertion
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) > 0 && trimmed[0] == '"' {
			a.Type = "llm"
			if err := json.Unmarshal(trimmed, &a.Text); err != nil {
				return fmt.Errorf("eval %q: assertions[%d]: %w", e.ID, i, err)
			}
		} else if err := json.Unmarshal(trimmed, &a); err != nil {
			return fmt.Errorf("eval %q: assertions[%d]: %w", e.ID, i, err)
		}
		e.Assertions = append(e.Assertions, a)
	}
	return nil
}

// EvalsFile is one authored evals document, in one of two mutually exclusive
// shapes. A skill suite is skill-creator's envelope, {skill_name?, evals},
// plus evolve's top-level Models restriction. An agent suite is {agent,
// evals}: the agent block (protocol and endpoints) is what makes it one, and
// skill_name/models are rejected beside it.
type EvalsFile struct {
	SkillName string `json:"skill_name,omitempty"`
	// Models restricts which models this skill's evals (and its sibling
	// triggers) may run, using the same tokens as the root models config
	// (provider ids, canonical model ids, or "all"). The effective set is the
	// intersection with the root models; an empty list means the root models.
	Models []string   `json:"models,omitempty"`
	Agent  *AgentSpec `json:"agent,omitempty"`
	Evals  []Eval     `json:"evals"`
}

// Kind reports which shape the file is: KindAgent with an agent block,
// KindSkill otherwise.
func (f *EvalsFile) Kind() string {
	if f.Agent != nil {
		return KindAgent
	}
	return KindSkill
}

// Restriction is the set of selection keys this suite may run on, in the
// tokens model.Model.MatchedBy understands: a skill suite's Models
// restriction (empty = every model), or an agent suite's protocol-qualified
// endpoint keys. It is the one input the shared applicability plumbing
// (plan.SkillCatalog.Models, plan.ApplicableEvals) takes for either kind.
func (f *EvalsFile) Restriction() []string {
	if f.Agent != nil {
		return f.Agent.EndpointKeys()
	}
	return f.Models
}

// Validate returns the problems (errors) and warnings in a loaded file:
// the shape's mutual exclusivity, the models restriction or agent block,
// and every eval under the rules of the file's kind.
func (f *EvalsFile) Validate() (problems, warnings []string) {
	if f.Agent != nil {
		if f.SkillName != "" {
			problems = append(problems, "skill_name does not belong in an agent suite (name the agent with agent.name)")
		}
		if len(f.Models) > 0 {
			problems = append(problems, "models does not belong in an agent suite (agent suites run on their endpoints)")
		}
		problems = append(problems, validateAgent(f.Agent)...)
	}
	problems = append(problems, ValidateModels(f.Models)...)
	problems = append(problems, ValidateEvals(f.Evals, f.Agent)...)
	if f.Agent != nil {
		for i, c := range f.Evals {
			warnings = append(warnings, responseWarnings(c, evalLabel(i, c))...)
		}
	}
	return problems, warnings
}

// LoadEvals parses an authored evals file in any supported format and
// normalizes it: expectations expand to llm assertions graded before the
// authored ones, and fixture paths resolve against the file's directory.
func LoadEvals(path string) (*EvalsFile, error) {
	var f EvalsFile
	if err := encfmt.DecodeFile(path, &f); err != nil {
		return nil, err
	}
	if err := f.normalize(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &f, nil
}

// normalize expands expectations into llm assertions (graded first, in
// authored order), resolves fixture refs per the staging rule, and defaults
// an agent block's protocol to ag-ui.
func (f *EvalsFile) normalize(baseDir string) error {
	if f.Agent != nil && f.Agent.Protocol == "" {
		f.Agent.Protocol = ProtocolAGUI
	}
	for i := range f.Evals {
		e := &f.Evals[i]
		if len(e.Expectations) > 0 {
			merged := make([]Assertion, 0, len(e.Expectations)+len(e.Assertions))
			for _, text := range e.Expectations {
				merged = append(merged, Assertion{Type: "llm", Text: text, FromExpectation: true})
			}
			e.Assertions = append(merged, e.Assertions...)
		}
		for j := range e.Files {
			ref := &e.Files[j]
			rel := strings.TrimPrefix(slashpath.Clean(filepath.ToSlash(ref.Rel)), "evals/")
			if !filepath.IsLocal(filepath.FromSlash(rel)) {
				return fmt.Errorf("eval %q: files[%d] %q escapes the evals directory", e.ID, j, ref.Rel)
			}
			ref.Source = filepath.Join(baseDir, filepath.FromSlash(rel))
			if rest, ok := strings.CutPrefix(rel, "files/"); ok {
				ref.Dest = filepath.FromSlash(rest)
			} else {
				ref.Dest = slashpath.Base(rel)
			}
		}
	}
	return nil
}

// ValidateModels returns the problems in an eval set's top-level models
// restriction. Tokens are resolved against the live registry at run time, so
// the only authored mistake to catch here is a blank entry.
func ValidateModels(models []string) []string {
	var problems []string
	for i, m := range models {
		if strings.TrimSpace(m) == "" {
			problems = append(problems, fmt.Sprintf("models[%d]: empty model restriction", i))
		}
	}
	return problems
}

// ValidateEvals returns the problems in a loaded (normalized) eval list:
// a skill suite's when agent is nil, an agent suite's otherwise. The
// agent-suite fields and assertion types are rejected in a skill suite, and
// an agent suite's protocol-specific fields are checked against its
// protocol.
func ValidateEvals(evals []Eval, agent *AgentSpec) []string {
	var problems []string
	seen := map[string]bool{}
	for i, c := range evals {
		label := evalLabel(i, c)
		switch {
		case c.ID == "":
			problems = append(problems, fmt.Sprintf("evals[%d]: missing id", i))
		case seen[c.ID]:
			problems = append(problems, fmt.Sprintf("%s: duplicate id", label))
		}
		seen[c.ID] = true
		if c.Prompt == "" {
			problems = append(problems, label+": missing prompt")
		}
		if len(c.Assertions) == 0 {
			problems = append(problems, label+": no expectations or assertions")
		}
		dests := map[string]string{}
		for j, ref := range c.Files {
			if info, err := os.Stat(ref.Source); err != nil || !info.Mode().IsRegular() {
				problems = append(problems, fmt.Sprintf("%s files[%d]: %s: fixture not found", label, j, ref.Rel))
			}
			if prev, ok := dests[ref.Dest]; ok {
				problems = append(problems, fmt.Sprintf("%s files[%d]: %s and %s both stage to %s",
					label, j, prev, ref.Rel, ref.Dest))
			}
			dests[ref.Dest] = ref.Rel
		}
		if agent != nil {
			problems = append(problems, validateAgentEval(c, agent.Protocol, label)...)
		} else if set := agentFieldsSet(c); len(set) > 0 {
			problems = append(problems, fmt.Sprintf("%s: %s only belong in an agent suite (an evals file with an "+
				"agent block)", label, strings.Join(set, ", ")))
		}
		for j, a := range c.Assertions {
			al := fmt.Sprintf("%s assertions[%d]", label, j)
			if agent == nil && slices.Contains(agentOnlyAssertions, a.Type) {
				problems = append(problems, fmt.Sprintf("%s: %s assertions only grade agent suites", al, a.Type))
				continue
			}
			problems = append(problems, validateAssertion(a, al)...)
		}
	}
	return problems
}

// evalLabel names an eval in messages: by id, else by position.
func evalLabel(i int, c Eval) string {
	if c.ID != "" {
		return fmt.Sprintf("eval %q", c.ID)
	}
	return fmt.Sprintf("evals[%d]", i)
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
	case "tool_call":
		if a.Tool == "" {
			problems = append(problems, label+": missing tool")
		}
		problems = append(problems, compileAll(label, "tool", a.Tool, "pattern", a.Pattern, "result", a.Result)...)
		problems = append(problems, validateBounds(a, label)...)
	case "tool_sequence":
		if len(a.Tools) == 0 {
			problems = append(problems, label+": missing tools (an ordered list of tool-name patterns)")
		}
		for j, expr := range a.Tools {
			if expr == "" {
				problems = append(problems, fmt.Sprintf("%s: tools[%d] is empty", label, j))
			}
			problems = append(problems, compileAll(label, fmt.Sprintf("tools[%d]", j), expr)...)
		}
	case "state":
		if _, err := agui.ParsePointer(a.Pointer); err != nil {
			problems = append(problems, fmt.Sprintf("%s: invalid pointer: %v", label, err))
		}
		if a.Equals != nil && a.Pattern != "" {
			problems = append(problems, label+": set equals or pattern, not both")
		}
		problems = append(problems, compileAll(label, "pattern", a.Pattern)...)
	case "hitl":
		problems = append(problems, compileAll(label, "tool", a.Tool, "reason", a.Reason, "pattern", a.Pattern)...)
		problems = append(problems, validateBounds(a, label)...)
	case "outcome":
		if !slices.Contains(agui.Outcomes, a.Expect) {
			problems = append(problems, fmt.Sprintf("%s: expect %q (want one of %s)",
				label, a.Expect, strings.Join(agui.Outcomes, ", ")))
		}
	case "conformance":
	case "llm":
		if a.Text == "" {
			problems = append(problems, label+": missing text")
		}
	default:
		problems = append(problems, fmt.Sprintf("%s: unknown assertion type %q", label, a.Type))
	}
	return problems
}

// compileAll checks every non-empty (name, expr) pair compiles as a regex.
func compileAll(label string, pairs ...string) []string {
	var problems []string
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			continue
		}
		if _, err := regexp.Compile(pairs[i+1]); err != nil {
			problems = append(problems, fmt.Sprintf("%s: invalid %s: %v", label, pairs[i], err))
		}
	}
	return problems
}

// validateBounds checks a match-count assertion's min/max.
func validateBounds(a Assertion, label string) []string {
	var problems []string
	if a.Min != nil && *a.Min < 0 {
		problems = append(problems, label+": min must be >= 0")
	}
	if a.Max != nil && *a.Max < 0 {
		problems = append(problems, label+": max must be >= 0")
	}
	if a.Min != nil && a.Max != nil && *a.Min > *a.Max {
		problems = append(problems, fmt.Sprintf("%s: min %d exceeds max %d", label, *a.Min, *a.Max))
	}
	return problems
}
