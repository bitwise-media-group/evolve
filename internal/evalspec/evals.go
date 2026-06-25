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

	"github.com/bitwise-media-group/evolve/internal/encfmt"
)

// Assertion is one graded condition of a behavioral eval. In the authored
// assertions array a bare string is shorthand for {type: "llm", text: ...}.
type Assertion struct {
	Type    string `json:"type"`
	Path    string `json:"path,omitempty"`
	Pattern string `json:"pattern,omitempty"`
	Run     string `json:"run,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	// Tool is a regex matched against an observed tool name for a tool_call
	// assertion (an MCP tool surfaces as "mcp__<server>__<tool>"); Pattern, when
	// set, is matched against the call's JSON-serialized arguments.
	Tool       string `json:"tool,omitempty"`
	Requires   string `json:"requires,omitempty"`
	ExpectExit *int   `json:"expect_exit,omitempty"`
	Text       string `json:"text,omitempty"`

	// FromExpectation marks an assertion expanded from the eval's
	// expectations list; results echo it as source: "expectation".
	FromExpectation bool `json:"-"`
}

// AssertionTypes is the closed set of supported assertion kinds.
var AssertionTypes = []string{"file_exists", "file_absent", "regex", "not_regex", "command", "tool_call", "llm"}

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
// deterministic assertions and run controls.
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
	AllowedTools   string      `json:"allowed_tools,omitempty"`
	SkipProviders  []string    `json:"skip_providers,omitempty"`
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

// EvalsFile is one authored evals document: skill-creator's envelope shape,
// {skill_name?, evals: [...]}.
type EvalsFile struct {
	SkillName string `json:"skill_name,omitempty"`
	Evals     []Eval `json:"evals"`
}

// SkipsProvider reports whether the eval opts out of a provider.
func (e Eval) SkipsProvider(name string) bool { return slices.Contains(e.SkipProviders, name) }

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
// authored order) and resolves fixture refs per the staging rule.
func (f *EvalsFile) normalize(baseDir string) error {
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

// ValidateEvals returns the problems in a loaded (normalized) eval list.
func ValidateEvals(evals []Eval) []string {
	var problems []string
	seen := map[string]bool{}
	for i, c := range evals {
		label := fmt.Sprintf("evals[%d]", i)
		if c.ID != "" {
			label = fmt.Sprintf("eval %q", c.ID)
		}
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
	case "tool_call":
		if a.Tool == "" {
			problems = append(problems, label+": missing tool")
		} else if _, err := regexp.Compile(a.Tool); err != nil {
			problems = append(problems, fmt.Sprintf("%s: invalid tool: %v", label, err))
		}
		if a.Pattern != "" {
			if _, err := regexp.Compile(a.Pattern); err != nil {
				problems = append(problems, fmt.Sprintf("%s: invalid pattern: %v", label, err))
			}
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
