// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package encfmt

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type doc struct {
	Name    string         `json:"name"`
	Tokens  int            `json:"tokens"`
	Rate    float64        `json:"rate"`
	Passed  *bool          `json:"passed"`
	Pricing *struct{}      `json:"pricing"`
	Tags    []string       `json:"tags,omitempty"`
	Extra   map[string]int `json:"extra,omitempty"`
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestDecodeEquivalence pins that the same document authored in every
// supported format decodes identically.
func TestDecodeEquivalence(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"d.json":  `{"name": "x", "tokens": 84852, "rate": 0.5, "passed": null, "pricing": null, "tags": ["a", "no"]}`,
		"d.jsonc": "// header\n{\"name\": \"x\", \"tokens\": 84852, \"rate\": 0.5, \"passed\": null, \"pricing\": null, \"tags\": [\"a\", \"no\"],}",
		"d.yaml":  "name: x\ntokens: 84852\nrate: 0.5\npassed: null\npricing: null\ntags: [a, \"no\"]\n",
		"d.yml":   "name: x\ntokens: 84852\nrate: 0.5\npassed:\npricing: ~\ntags:\n  - a\n  - \"no\"\n",
	}
	var want doc
	if err := DecodeFile(write(t, dir, "d.json", files["d.json"]), &want); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		var got doc
		if err := DecodeFile(write(t, dir, name, content), &got); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s decoded %+v, want %+v", name, got, want)
		}
	}
}

func TestMarshalRoundTrips(t *testing.T) {
	in := doc{Name: "x", Tokens: 84852, Rate: 0.5, Tags: []string{"a", "no"}}
	for _, ext := range []string{"json", "jsonc", "yaml", "yml"} {
		data, err := Marshal(in, ext, "maintained by evolve")
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		path := write(t, t.TempDir(), "d."+Canonical(ext), string(data))
		var out doc
		if err := DecodeFile(path, &out); err != nil {
			t.Fatalf("%s reload: %v", ext, err)
		}
		if !reflect.DeepEqual(in, out) {
			t.Errorf("%s round-trip = %+v, want %+v", ext, out, in)
		}
	}
}

// TestYAMLKeepsIntegersIntegral guards the UseNumber normalization: token
// counts must not render as floats or quoted strings.
func TestYAMLKeepsIntegersIntegral(t *testing.T) {
	data, err := Marshal(doc{Tokens: 84852}, "yaml", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "tokens: 84852\n") {
		t.Errorf("yaml = %q, want bare integer", data)
	}
}

// TestYAMLExplicitNullSurvives keeps the v1 convention: pricing null stays
// an explicit null in yaml, and ambiguous strings stay quoted.
func TestYAMLExplicitNullSurvives(t *testing.T) {
	data, err := Marshal(doc{Tags: []string{"no", "on", "3.0"}}, "yaml", "")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "pricing: null") || !strings.Contains(s, "passed: null") {
		t.Errorf("yaml = %q, want explicit nulls", s)
	}
	var out doc
	if err := DecodeFile(write(t, t.TempDir(), "d.yaml", s), &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Tags, []string{"no", "on", "3.0"}) {
		t.Errorf("tags = %+v, want yaml-ambiguous strings preserved", out.Tags)
	}
}

func TestJSONCHeaderComment(t *testing.T) {
	data, err := Marshal(doc{}, "jsonc", "maintained by evolve")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "// maintained by evolve\n{") {
		t.Errorf("jsonc = %q, want comment header", data)
	}
}

func TestNormalizeRejectsNonStringKeys(t *testing.T) {
	path := write(t, t.TempDir(), "d.yaml", "1: x\n")
	if _, err := NormalizeToJSON(path); err == nil ||
		!strings.Contains(err.Error(), "non-string mapping key") {
		t.Errorf("err = %v, want non-string key error", err)
	}
}

func TestFindOne(t *testing.T) {
	dir := t.TempDir()
	if path, err := FindOne(dir, "evals"); err != nil || path != "" {
		t.Errorf("empty dir = (%q, %v)", path, err)
	}
	write(t, dir, "evals.yaml", "evals: []\n")
	path, err := FindOne(dir, "evals")
	if err != nil || filepath.Base(path) != "evals.yaml" {
		t.Errorf("single = (%q, %v)", path, err)
	}
	write(t, dir, "evals.json", "{}")
	if _, err := FindOne(dir, "evals"); err == nil ||
		!strings.Contains(err.Error(), "keep exactly one") {
		t.Errorf("ambiguous err = %v", err)
	}
}

func TestMarshalRejectsUnknownFormat(t *testing.T) {
	if _, err := Marshal(doc{}, "toml", ""); err == nil {
		t.Error("toml must be rejected")
	}
}
