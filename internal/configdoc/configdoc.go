// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package configdoc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/run"
)

// Option documents one leaf configuration key.
type Option struct {
	Key      string // dotted path, e.g. "checks.max_skill_lines"
	Type     string // human-readable type for the reference table
	Value    any    // built-in default; nil when the default is behavioral
	Example  any    // value rendered commented-out when Value is nil
	Fallback string // what an absent key means, when Value is nil
	Doc      string // one-line description
}

// Schema returns every documented option in render order. Defaults come from
// the same code the engines use, so the generated docs cannot drift.
func Schema() []Option {
	checks := run.DefaultCheckConfig()
	return []Option{
		{
			Key: "layout", Type: "string", Value: "auto",
			Doc: "Repository layout: auto, marketplace, multi, or single.",
		},
		{
			Key: "default_models", Type: "list of strings", Value: []string{"anthropic"},
			Doc: "Model spec used when --models is omitted: provider names, model ids, " +
				"provider-qualified ids (cursor/sonnet-4.5), or all.",
		},
		{
			Key: "cache_dir", Type: "string", Example: "~/.cache/evolve",
			Fallback: "the OS user cache dir",
			Doc:      "Directory holding the token-count cache.",
		},
		{
			Key: "results_format", Type: "string", Value: "json",
			Doc: "Format for committed results files and the EVALUATION rollup: json, jsonc, or yaml.",
		},
		{
			Key: "checks.license", Type: "string", Example: "MIT",
			Fallback: "the license field is forbidden",
			Doc:      "License every SKILL.md must declare; when unset, skills must not declare one.",
		},
		{
			Key: "checks.description_pattern", Type: "string", Value: checks.TriggerPattern,
			Doc: "Regex every skill description must match.",
		},
		{
			Key: "checks.max_skill_lines", Type: "int", Value: checks.MaxSkillLines,
			Doc: "Maximum SKILL.md line count.",
		},
		{
			Key: "checks.require_codex_manifest", Type: "bool", Value: checks.RequireCodexManifest,
			Doc: "Require .codex-plugin/plugin.json beside Claude's manifest.",
		},
		{
			Key: "checks.forbid_hooks", Type: "bool", Value: checks.ForbidHooks,
			Doc: "Forbid a hooks/ directory in plugins.",
		},
		{
			Key: "checks.marketplace", Type: "bool", Value: checks.Marketplace,
			Doc: "Validate marketplace manifests (marketplace layout only).",
		},
		{
			Key: "report.thresholds.triggers_min_pass_rate", Type: "float", Example: 0.8,
			Fallback: "no gate",
			Doc:      "Minimum triggers pass rate (0-1); report --check exits 1 below it.",
		},
		{
			Key: "report.thresholds.evals_min_pass_rate", Type: "float", Example: 0.9,
			Fallback: "no gate",
			Doc:      "Minimum evals pass rate (0-1); report --check exits 1 below it.",
		},
		{
			Key: "report.thresholds.models", Type: "list of strings", Example: []string{"anthropic/claude-fable-5"},
			Fallback: "every model with stored results",
			Doc:      "Model keys (provider/model-id) the thresholds apply to.",
		},
	}
}

// The providers map holds arbitrary provider names, so it renders as a
// commented example block rather than leaf options.
const (
	providersDoc = "Per-provider overrides: providers.<name>.models replaces that provider's " +
		"builtin model matrix (model ids, display names, USD-per-mtok pricing)."
	providersFallback = "every provider keeps its builtin models"
)

// commentWidth caps comment lines in the generated examples.
const commentWidth = 72

// Markdown renders the configuration reference page.
func Markdown() []byte {
	var b strings.Builder
	b.WriteString("# Configuration\n" +
		"\n" +
		"evolve reads an optional config file named `.evolve.<ext>` from the repository root (`--root`),\n" +
		"where `<ext>` is one of `yaml`, `yml`, `json`, or `jsonc`. At most one config file may\n" +
		"exist — two formats side by side is an error. Settings layer lowest precedence first: built-in\n" +
		"defaults, the config file, `EVOLVE_*` environment variables, then explicit flags.\n" +
		"\n" +
		"## Options\n" +
		"\n" +
		"| Key | Type | Default | Description |\n" +
		"| --- | --- | --- | --- |\n")
	for _, o := range Schema() {
		def := "unset — " + o.Fallback
		if o.Value != nil {
			def = "`" + jsonValue(o.Value) + "`"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n",
			mdCell(o.Key), mdCell(o.Type), mdCell(def), mdCell(o.Doc))
	}
	b.WriteString("\n" +
		"## Provider overrides\n" +
		"\n" +
		"`providers.<name>.models` replaces that provider's builtin model matrix; providers without an\n" +
		"entry keep their builtin models. Each list entry is an object:\n" +
		"\n" +
		"| Field | Type | Description |\n" +
		"| --- | --- | --- |\n" +
		"| `id` | string | Model id passed to the runner CLI (required). |\n" +
		"| `display` | string | Human-readable name shown in reports (default: the id). |\n" +
		"| `input_per_mtok` | float | Input price in USD per million tokens (omit when unpublished). |\n" +
		"| `output_per_mtok` | float | Output price in USD per million tokens (omit when unpublished). |\n" +
		"\n" +
		"## Annotated examples\n" +
		"\n" +
		"Generated alongside this page, each with every default value set and a comment per option —\n" +
		"copy one to the repository root:\n" +
		"\n" +
		"- [`.evolve.yaml`](.evolve.yaml)\n" +
		"- [`.evolve.jsonc`](.evolve.jsonc)\n")
	return []byte(b.String())
}

// ExampleYAML renders the annotated .evolve.yaml example.
func ExampleYAML() []byte {
	var b strings.Builder
	b.WriteString(header("#"))
	writeYAML(&b, tree(), 0)
	b.WriteString("\n")
	writeComment(&b, "# ", providersBlockDoc())
	b.WriteString(`# providers:
#   cursor:
#     models:
#       - id: "sonnet-4.5"
#         display: "Cursor - Sonnet 4.5"
#         input_per_mtok: 3.0
#         output_per_mtok: 15.0
`)
	return []byte(b.String())
}

// ExampleJSONC renders the annotated .evolve.jsonc example. The loader
// standardizes JSONC before parsing, so comments and trailing commas are
// both tolerated.
func ExampleJSONC() []byte {
	var b strings.Builder
	b.WriteString(header("//"))
	b.WriteString("{\n")
	writeJSONC(&b, tree(), 1)
	b.WriteString("\n")
	writeComment(&b, "  // ", providersBlockDoc())
	b.WriteString(`  // "providers": {
  //   "cursor": {
  //     "models": [
  //       { "id": "sonnet-4.5", "display": "Cursor - Sonnet 4.5", "input_per_mtok": 3.0, "output_per_mtok": 15.0 }
  //     ]
  //   }
  // }
}
`)
	return []byte(b.String())
}

// node is one entry in an example file: a section with children or a leaf
// with a value. Commented leaves render as annotated suggestions so the
// loader's absent-means-default behavior stays intact.
type node struct {
	key       string
	doc       []string
	value     any
	commented bool
	children  []*node
}

// tree shapes the flat schema into nested sections, preserving order.
func tree() []*node {
	var roots []*node
	sections := map[string]*node{}
	for _, o := range Schema() {
		parts := strings.Split(o.Key, ".")
		var parent *node
		for i := range parts[:len(parts)-1] {
			path := strings.Join(parts[:i+1], ".")
			s := sections[path]
			if s == nil {
				s = &node{key: parts[i]}
				sections[path] = s
				if parent == nil {
					roots = append(roots, s)
				} else {
					parent.children = append(parent.children, s)
				}
			}
			parent = s
		}
		leaf := &node{key: parts[len(parts)-1], doc: comments(o), commented: o.Value == nil}
		if leaf.value = o.Value; leaf.commented {
			leaf.value = o.Example
		}
		if parent == nil {
			roots = append(roots, leaf)
		} else {
			parent.children = append(parent.children, leaf)
		}
	}
	return roots
}

// comments renders an option's annotation: the wrapped description plus a
// Default line for keys the examples leave commented out.
func comments(o Option) []string {
	lines := wrap(o.Doc, commentWidth)
	if o.Value == nil {
		lines = append(lines, "Default: unset — "+o.Fallback+".")
	}
	return lines
}

// providersBlockDoc is the annotation shared by every providers example.
func providersBlockDoc() []string {
	return append(wrap(providersDoc, commentWidth), "Default: unset — "+providersFallback+".")
}

func header(comment string) string {
	return comment + " evolve configuration — every value below is the built-in default.\n" +
		comment + " Generated by `evolve docs --format config`.\n"
}

func writeComment(b *strings.Builder, prefix string, lines []string) {
	for _, l := range lines {
		b.WriteString(prefix)
		b.WriteString(l)
		b.WriteString("\n")
	}
}

func writeYAML(b *strings.Builder, nodes []*node, depth int) {
	indent := strings.Repeat("  ", depth)
	for i, n := range nodes {
		if i > 0 || depth == 0 {
			b.WriteString("\n")
		}
		writeComment(b, indent+"# ", n.doc)
		switch {
		case n.children != nil:
			b.WriteString(indent)
			b.WriteString(n.key)
			b.WriteString(":\n")
			writeYAML(b, n.children, depth+1)
		case n.commented:
			b.WriteString(indent)
			b.WriteString("# ")
			b.WriteString(n.key)
			b.WriteString(": ")
			b.WriteString(jsonValue(n.value))
			b.WriteString("\n")
		default:
			b.WriteString(indent)
			b.WriteString(n.key)
			b.WriteString(": ")
			b.WriteString(jsonValue(n.value))
			b.WriteString("\n")
		}
	}
}

func writeJSONC(b *strings.Builder, nodes []*node, depth int) {
	indent := strings.Repeat("  ", depth)
	last := -1
	for i, n := range nodes {
		if !n.commented {
			last = i
		}
	}
	for i, n := range nodes {
		if i > 0 {
			b.WriteString("\n")
		}
		writeComment(b, indent+"// ", n.doc)
		comma := ","
		if i == last {
			comma = ""
		}
		switch {
		case n.children != nil:
			b.WriteString(indent)
			b.WriteString(jsonValue(n.key))
			b.WriteString(": {\n")
			writeJSONC(b, n.children, depth+1)
			b.WriteString(indent)
			b.WriteString("}")
			b.WriteString(comma)
			b.WriteString("\n")
		case n.commented:
			b.WriteString(indent)
			b.WriteString("// ")
			b.WriteString(jsonValue(n.key))
			b.WriteString(": ")
			b.WriteString(jsonValue(n.value))
			b.WriteString(",\n")
		default:
			b.WriteString(indent)
			b.WriteString(jsonValue(n.key))
			b.WriteString(": ")
			b.WriteString(jsonValue(n.value))
			b.WriteString(comma)
			b.WriteString("\n")
		}
	}
}

// jsonValue renders v as a JSON literal, which is also valid in YAML flow
// context. Schema values are static, so encoding cannot fail.
func jsonValue(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// mdCell escapes pipes so rendered values cannot break the table row.
func mdCell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// wrap splits s into space-separated lines of at most width bytes.
func wrap(s string, width int) []string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(s) {
		switch {
		case line == "":
			line = w
		case len(line)+1+len(w) <= width:
			line += " " + w
		default:
			lines = append(lines, line)
			line = w
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
