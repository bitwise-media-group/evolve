// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package encfmt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tailscale/hujson"
	yaml "go.yaml.in/yaml/v3"
)

// AppendToList appends items to the array at the nested object path keys in
// the file at path, creating missing intermediate objects (and the array
// itself) while preserving the file's comments. The format is chosen by
// extension as elsewhere in this package; items are encoded through their
// json tags in every format, matching Marshal. A missing or empty file is
// treated as an empty document. JSON/JSONC output is reformatted by hujson's
// canonical formatter after patching; YAML keeps its own layout.
func AppendToList(path string, keys []string, items []any) error {
	if len(keys) == 0 || len(items) == 0 {
		return fmt.Errorf("AppendToList: keys and items must be non-empty")
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	perm := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		perm = info.Mode().Perm()
	}

	// Encode items through the JSON data model up front so both branches write
	// the same shape (yaml.Node.Encode would otherwise use yaml tags).
	models := make([]any, 0, len(items))
	for _, it := range items {
		m, err := jsonModel(it)
		if err != nil {
			return err
		}
		models = append(models, m)
	}

	var out []byte
	switch Canonical(strings.TrimPrefix(filepath.Ext(path), ".")) {
	case "json", "jsonc":
		out, err = appendHuJSON(data, keys, models)
	case "yaml":
		out, err = appendYAML(data, keys, models)
	default:
		return fmt.Errorf("%s: unsupported extension (want .%s)", path, strings.Join(Extensions, ", ."))
	}
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return os.WriteFile(path, out, perm)
}

// appendHuJSON applies an RFC 6902 patch through hujson so comments and
// trailing commas in the surrounding document survive the edit.
func appendHuJSON(data []byte, keys []string, items []any) ([]byte, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}")
	}
	v, err := hujson.Parse(data)
	if err != nil {
		return nil, err
	}

	// Walk a standardized copy to find how much of the key path already
	// exists: existing arrays take per-item appends; the first missing key
	// takes one add op carrying the rest of the path as nested objects.
	std, err := hujson.Standardize(bytes.Clone(data))
	if err != nil {
		return nil, err
	}
	var plain any
	if err := json.Unmarshal(std, &plain); err != nil {
		return nil, err
	}
	depth := 0
	cur := plain
	for _, key := range keys {
		obj, ok := cur.(map[string]any)
		if !ok {
			if cur == nil {
				break // a null value is overwritten by the add below
			}
			return nil, fmt.Errorf("%s is not an object", strings.Join(keys[:depth], "."))
		}
		val, ok := obj[key]
		if !ok {
			break
		}
		cur = val
		depth++
	}

	type op struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	}
	var ops []op
	if depth == len(keys) {
		if _, ok := cur.([]any); !ok && cur != nil {
			return nil, fmt.Errorf("%s is not an array", strings.Join(keys, "."))
		}
		if cur == nil {
			ops = append(ops, op{"add", jsonPointer(keys), items})
		} else {
			for _, it := range items {
				ops = append(ops, op{"add", jsonPointer(keys) + "/-", it})
			}
		}
	} else {
		var val any = items
		for i := len(keys) - 1; i > depth; i-- {
			val = map[string]any{keys[i]: val}
		}
		ops = append(ops, op{"add", jsonPointer(keys[:depth+1]), val})
	}
	patch, err := json.Marshal(ops)
	if err != nil {
		return nil, err
	}
	if err := v.Patch(patch); err != nil {
		return nil, err
	}
	v.Format()
	return v.Pack(), nil
}

// jsonPointer renders keys as an RFC 6901 pointer.
func jsonPointer(keys []string) string {
	var b strings.Builder
	for _, k := range keys {
		b.WriteByte('/')
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1"))
	}
	return b.String()
}

// appendYAML edits the parsed node tree in place — comments and the existing
// layout ride along on the untouched nodes when the document re-encodes.
func appendYAML(data []byte, keys []string, items []any) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		doc = yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}},
		}
	}
	cur := doc.Content[0]
	if err := asMapping(cur, "document root"); err != nil {
		return nil, err
	}
	for i, key := range keys {
		last := i == len(keys)-1
		val := mappingValue(cur, key)
		if val == nil {
			val = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			if last {
				val = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			}
			cur.Content = append(cur.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, val)
		}
		if last {
			if err := asSequence(val, strings.Join(keys, ".")); err != nil {
				return nil, err
			}
			for _, it := range items {
				n := &yaml.Node{}
				if err := n.Encode(it); err != nil {
					return nil, err
				}
				val.Content = append(val.Content, n)
			}
			break
		}
		if err := asMapping(val, strings.Join(keys[:i+1], ".")); err != nil {
			return nil, err
		}
		cur = val
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// mappingValue returns the value node for key in mapping m, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// asMapping ensures n is (or, for an explicit null, becomes) a mapping node.
func asMapping(n *yaml.Node, what string) error {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		n.Kind, n.Tag, n.Value = yaml.MappingNode, "!!map", ""
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("%s is not a mapping", what)
	}
	return nil
}

// asSequence ensures n is (or, for an explicit null, becomes) a sequence node.
func asSequence(n *yaml.Node, what string) error {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		n.Kind, n.Tag, n.Value = yaml.SequenceNode, "!!seq", ""
		return nil
	}
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s is not a list", what)
	}
	return nil
}
