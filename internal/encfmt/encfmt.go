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

// Extensions lists the supported file extensions in discovery order.
var Extensions = []string{"json", "jsonc", "yaml", "yml"}

// Formats lists the valid emission formats; yml is accepted on read only and
// canonicalizes to yaml.
var Formats = []string{"json", "jsonc", "yaml"}

// Canonical collapses the yml alias; every other extension names itself.
func Canonical(ext string) string {
	if ext == "yml" {
		return "yaml"
	}
	return ext
}

// NormalizeToJSON reads the file at path and returns its content as JSON
// bytes, dispatching on the extension: .json verbatim, .jsonc standardized
// via hujson, .yaml/.yml decoded generically and re-marshaled.
func NormalizeToJSON(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.TrimPrefix(filepath.Ext(path), ".") {
	case "json":
		return data, nil
	case "jsonc":
		std, err := hujson.Standardize(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return std, nil
	case "yaml", "yml":
		var v any
		if err := yaml.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("%s: not JSON-representable (non-string mapping key?): %w", path, err)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s: unsupported extension (want .%s)", path, strings.Join(Extensions, ", ."))
}

// DecodeFile decodes the file at path into out (a json-tagged value),
// accepting any supported format.
func DecodeFile(path string, out any) error {
	data, err := NormalizeToJSON(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// Marshal renders v in the format named by ext (json, jsonc, or yaml; yml
// canonicalizes to yaml). comment, when non-empty, becomes a header line in
// the formats that support comments (jsonc, yaml) and is dropped for json.
func Marshal(v any, ext, comment string) ([]byte, error) {
	switch Canonical(ext) {
	case "json":
		return marshalJSON(v)
	case "jsonc":
		data, err := marshalJSON(v)
		if err != nil {
			return nil, err
		}
		if comment != "" {
			data = append([]byte("// "+comment+"\n"), data...)
		}
		return data, nil
	case "yaml":
		model, err := jsonModel(v)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if comment != "" {
			buf.WriteString("# " + comment + "\n")
		}
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(model); err != nil {
			return nil, err
		}
		if err := enc.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	return nil, fmt.Errorf("unsupported format %q (want %s)", ext, strings.Join(Formats, ", "))
}

// FindOne locates dir/stem.<ext> for exactly one supported extension: ""
// when none exists, an error listing every candidate when several do.
func FindOne(dir, stem string) (string, error) {
	var found []string
	for _, ext := range Extensions {
		path := filepath.Join(dir, stem+"."+ext)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			found = append(found, path)
		}
	}
	switch len(found) {
	case 0:
		return "", nil
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("ambiguous %s: found %s; keep exactly one", stem, strings.Join(found, ", "))
}

func marshalJSON(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// jsonModel converts v to the generic JSON data model — the json-tag keys,
// integers kept integral, explicit nulls preserved — so the yaml encoder
// emits exactly what a JSON reader of the same value would see.
func jsonModel(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return integralNumbers(out), nil
}

// integralNumbers replaces json.Number leaves with int64 or float64 so token
// counts stay integers instead of rendering as floats (or quoted strings).
func integralNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = integralNumbers(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = integralNumbers(val)
		}
		return t
	case json.Number:
		if !strings.ContainsAny(t.String(), ".eE") {
			if i, err := t.Int64(); err == nil {
				return i
			}
		}
		f, _ := t.Float64()
		return f
	}
	return v
}
