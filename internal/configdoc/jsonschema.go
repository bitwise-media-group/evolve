// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package configdoc

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/encfmt"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/report"
)

// schemaID is the published $id of the configuration schema: the raw URL of
// the generated file under docs/config, so an editor can resolve it by link.
const schemaID = "https://raw.githubusercontent.com/bitwise-media-group/evolve/main/docs/config/config.schema.json"

// schemaDescription documents the instance the schema validates.
const schemaDescription = "Optional .evolve.<ext> configuration for an evolve repository " +
	"(yaml, yml, json, or jsonc at the root; at most one). Generated from the viper config by " +
	"`evolve docs --format config`; do not edit by hand."

// JSONSchema renders the JSON Schema (draft 2020-12) for the .evolve.<ext>
// config file. Every key, type, and default is derived from the same Schema()
// the docs and example generators use, so the schema cannot drift from the
// loader: add a config option once and all three outputs gain it.
func JSONSchema() []byte {
	props := obj{}
	props.set("$schema", leaf().
		set("description", "Optional URI of this schema; ignored by evolve, honored by editors.").
		set("type", "string"))
	derived := deriveProperties()
	for i, k := range derived.keys {
		props.set(k, derived.vals[i])
	}
	props.set("providers", providersSchema())

	defs := obj{}
	defs.set("model", modelDef())

	root := obj{}
	root.set("$schema", "https://json-schema.org/draft/2020-12/schema")
	root.set("$id", schemaID)
	root.set("title", "evolve configuration")
	root.set("description", schemaDescription)
	root.set("type", "object")
	root.set("properties", &props)
	root.set("additionalProperties", false)
	root.set("$defs", &defs)
	return render(&root)
}

// deriveProperties shapes the flat Schema() into nested JSON Schema property
// nodes, preserving authored order. Intermediate keys (checks, report,
// report.thresholds) become object nodes; leaves carry the type, doc, default,
// and any enum/range constraint. Sections accept null so an empty section
// (every child commented out, as YAML renders it) still validates.
func deriveProperties() *obj {
	root := &obj{}
	sections := map[string]*obj{"": root}
	for _, o := range Schema() {
		parts := strings.Split(o.Key, ".")
		parent, prefix := root, ""
		for _, seg := range parts[:len(parts)-1] {
			if prefix != "" {
				prefix += "."
			}
			prefix += seg
			childProps, ok := sections[prefix]
			if !ok {
				childProps = &obj{}
				sections[prefix] = childProps
				parent.set(seg, leaf().
					set("type", []string{"object", "null"}).
					set("properties", childProps).
					set("additionalProperties", false))
			}
			parent = childProps
		}
		parent.set(parts[len(parts)-1], leafSchema(o))
	}
	return root
}

// leafSchema builds the schema node for one configuration leaf.
func leafSchema(o Option) *obj {
	s := leaf().set("description", o.Doc)
	enum := enumValues(o.Key)
	switch o.Type {
	case "string":
		s.set("type", "string")
	case "int":
		s.set("type", "integer")
	case "bool":
		s.set("type", "boolean")
	case "float":
		s.set("type", "number")
	case "list of strings":
		items := leaf().set("type", "string")
		if enum != nil {
			// A list's enum constrains its items, not the array itself.
			items.set("enum", enum)
			enum = nil
		}
		s.set("type", "array").set("items", items)
	default:
		panic("configdoc: unhandled option type " + o.Type)
	}
	if enum != nil {
		s.set("enum", enum)
	}
	if isPassRate(o.Key) {
		s.set("minimum", 0).set("maximum", 1)
	}
	if o.Value != nil {
		s.set("default", o.Value)
	}
	return s
}

// enumValues lists the closed value set for the keys that have one, sourced
// from the same constants the loader validates against so the enum cannot
// drift. layout.Auto is the empty marker; the config spells it "auto".
func enumValues(key string) any {
	switch key {
	case "layout":
		return []string{"auto", string(layout.Marketplace), string(layout.Multi), string(layout.Single)}
	case "results_format":
		return encfmt.Formats
	case "report.thresholds.maturity":
		return []string{
			string(report.MaturityStable),
			string(report.MaturityUnstable),
			string(report.MaturityPrerelease),
		}
	}
	return nil
}

// isPassRate reports whether key is a 0-1 pass-rate threshold.
func isPassRate(key string) bool {
	return key == "report.thresholds.triggers_min_pass_rate" ||
		key == "report.thresholds.evals_min_pass_rate"
}

// providersSchema describes the providers.<name>.models override map. Provider
// names are arbitrary, so they live under additionalProperties; each entry's
// models list reuses the shared model definition.
func providersSchema() *obj {
	models := leaf().
		set("description", "Replacement model matrix for this provider.").
		set("type", "array").
		set("items", leaf().set("$ref", "#/$defs/model"))
	perProvider := leaf().
		set("type", "object").
		set("properties", leaf().set("models", models)).
		set("additionalProperties", false)
	return leaf().
		set("description", providersDoc).
		set("type", "object").
		set("additionalProperties", perProvider)
}

// modelDef is the shared $defs entry for one model in a provider's matrix. The
// field docs mirror the provider-overrides table in Markdown().
func modelDef() *obj {
	props := leaf().
		set("id", leaf().
			set("description", "Model id passed to the runner CLI (required).").
			set("type", "string").
			set("minLength", 1)).
		set("display", leaf().
			set("description", "Human-readable name shown in reports (default: the id).").
			set("type", "string")).
		set("input_per_mtok", leaf().
			set("description", "Input price in USD per million tokens (omit when unpublished).").
			set("type", "number")).
		set("output_per_mtok", leaf().
			set("description", "Output price in USD per million tokens (omit when unpublished).").
			set("type", "number"))
	return leaf().
		set("description", "One model in a provider's matrix.").
		set("type", "object").
		set("required", []string{"id"}).
		set("properties", props).
		set("additionalProperties", false)
}

// obj is an insertion-ordered JSON object, so the generated schema lists keys
// in a curated order (and byte-stably) rather than Go's map-sorted order.
type obj struct {
	keys []string
	vals []any
}

// leaf returns a fresh ordered object for fluent construction.
func leaf() *obj { return &obj{} }

// set appends key with value and returns the object for chaining. A repeated
// key would duplicate; the schema builders never set the same key twice.
func (o *obj) set(key string, val any) *obj {
	o.keys = append(o.keys, key)
	o.vals = append(o.vals, val)
	return o
}

// MarshalJSON emits the members in insertion order with HTML escaping off, so
// the `<name>` and regex literals in the docs survive unescaped.
func (o *obj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := marshalNoEscape(k)
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		vb, err := marshalNoEscape(o.vals[i])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// marshalNoEscape encodes v compactly without HTML escaping. Nested *obj
// values recurse through this same path via their MarshalJSON.
func marshalNoEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// render indents the schema with two spaces and a trailing newline. The schema
// is static, so encoding cannot fail.
func render(root *obj) []byte {
	compact, err := marshalNoEscape(root)
	if err != nil {
		panic(err)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		panic(err)
	}
	out.WriteByte('\n')
	return out.Bytes()
}
