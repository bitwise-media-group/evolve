// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package configdoc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/model"
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
			Key: "models", Type: "list of strings", Example: []string{"anthropic/claude-sonnet-4-6"},
			Fallback: "every model runnable by an available harness",
			Doc: "Restriction on which models exist: provider ids, canonical model ids " +
				"(anthropic/claude-sonnet-4-6), or all. Unlisted models are unavailable. --model filters within it.",
		},
		{
			Key: "harnesses", Type: "list of strings", Example: []string{"claude", "copilot"},
			Fallback: "every harness found on PATH",
			Doc: "Restriction on which agent CLIs (claude, codex, gemini, cursor, copilot, antigravity) " +
				"may drive models. --harness filters within it.",
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
			Key: "telemetry.dir", Type: "string", Example: "./telemetry",
			Fallback: "telemetry disabled",
			Doc: "Directory for the OpenTelemetry JSON exporter (traces.json, metrics.json, logs.json); " +
				"the --telemetry-dir flag overrides it and both win over OTEL_* env vars.",
		},
		{
			Key: "max_turns", Type: "int", Value: model.DefaultMaxTurns,
			Doc: "Default maximum agent turns per behavioral eval; --max-turns and a per-eval max_turns override it.",
		},
		{
			Key: "baseline", Type: "bool", Value: true,
			Doc: "Benchmark each eval without the skill (the skill's lift over no skill), recomputed only " +
				"when the eval or its fixtures change. --baseline overrides for one run.",
		},
		{
			Key: "stale_results", Type: "string", Example: "keep",
			Fallback: "prompt on a terminal, otherwise keep",
			Doc: "How run/report treat stored results for models outside the `models` restriction: keep or drop. " +
				"--stale-results overrides.",
		},
		{
			Key: "sandbox.enabled", Type: "bool", Value: true,
			Doc: "Confine agent writes with an OS sandbox (sandbox-exec on macOS, bubblewrap on Linux); " +
				"--no-sandbox overrides for one run.",
		},
		{
			Key: "sandbox.protected_roots", Type: "list of strings", Example: []string{"~/Repos"},
			Fallback: "the parent directory of the repository under test",
			Doc: "Directories kept read-only to agent runs so an escaping agent cannot modify other " +
				"source repositories; the workspace stays writable. Reads, the network, and tool caches " +
				"outside these roots are unaffected.",
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
			Key: "checks.ideal_skill_lines", Type: "int", Value: checks.Signals.IdealSkillLines,
			Doc: "Ideal SKILL.md line count for the advisory size signal (full at or below; zero at the cap).",
		},
		{
			Key: "checks.signals", Type: "bool", Value: checks.Signals.Enabled,
			Doc: "Emit the advisory skill-quality signals after run checks; the --no-signals flag forces them off.",
		},
		{
			Key: "checks.plugin_manifests", Type: "list of strings", Value: checks.PluginManifests,
			Doc: "Plugin manifests every plugin must ship: claude (.claude-plugin/plugin.json) and/or " +
				"codex (.codex-plugin/plugin.json). With both, a hooks/ directory is forbidden " +
				"(codex and claude hooks.json are incompatible).",
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
		{
			Key: "report.junit", Type: "string", Example: "coverage/junit.xml",
			Fallback: "no JUnit file written",
			Doc: "Path for a JUnit XML test-results file (one testcase per eval/trigger case per model); " +
				"the --junit flag overrides it.",
		},
		{
			Key: "report.cobertura", Type: "string", Example: "coverage/cobertura-coverage.xml",
			Fallback: "no Cobertura file written",
			Doc: "Path for a Cobertura XML coverage file marking each skill covered by a current eval result; " +
				"the --cobertura flag overrides it.",
		},
		{
			Key: "report.strict", Type: "bool", Value: false,
			Doc: "Require the configured model matrix: report --check holds every defined model to the thresholds, " +
				"and the Cobertura output covers a skill only when every defined model has a current result. " +
				"The --strict flag overrides it.",
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

// Markdown renders the configuration reference table. It is embedded into
// docs/config/index.md through a pymdownx.snippets marker, so it is a bare
// fragment carrying no page heading of its own.
func Markdown() []byte {
	var b strings.Builder
	b.WriteString("| Key | Type | Default | Description |\n" +
		"| --- | --- | --- | --- |\n")
	for _, o := range Schema() {
		def := "unset — " + o.Fallback
		if o.Value != nil {
			def = "`" + jsonValue(o.Value) + "`"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n",
			mdCell(o.Key), mdCell(o.Type), mdCell(def), mdCell(o.Doc))
	}
	return []byte(b.String())
}

// ExampleYAML renders the annotated .evolve.yaml example. The leading
// yaml-language-server directive is a comment, so it adds editor validation
// without becoming a parsed key.
func ExampleYAML() []byte {
	var b strings.Builder
	b.WriteString("# yaml-language-server: $schema=" + schemaID + "\n")
	b.WriteString(header("#"))
	writeYAML(&b, tree(), 0)
	b.WriteString("\n")
	writeComment(&b, "# ", providersBlockDoc())
	b.WriteString(`# providers:
#   cursor:
#     models:
#       - id: "composer-2.5"
#         display: "Cursor Composer 2.5"
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
	fmt.Fprintf(&b, "  %s: %s,\n\n", jsonValue("$schema"), jsonValue(schemaID))
	writeJSONC(&b, tree(), 1)
	b.WriteString("\n")
	writeComment(&b, "  // ", providersBlockDoc())
	b.WriteString(`  // "providers": {
  //   "cursor": {
  //     "models": [
  //       { "id": "composer-2.5", "display": "Cursor Composer 2.5", "input_per_mtok": 3.0, "output_per_mtok": 15.0 }
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
	for w := range strings.FieldsSeq(s) {
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
