// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package encfmt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type appendEntry struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// writeAppend seeds path with content (skipped when empty), runs AppendToList,
// and returns the resulting file.
func writeAppend(t *testing.T, path, content string, keys []string, items ...any) string {
	t.Helper()
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := AppendToList(path, keys, items); err != nil {
		t.Fatalf("AppendToList: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestAppendToListYAMLPreservesComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".evolve.yaml")
	in := "# repo eval config\nmodels: [\"anthropic/*\"] # keep\nproviders:\n  anthropic:\n    models:\n      - id: claude-sonnet-4-6 # pinned\n"
	out := writeAppend(t, path, in, []string{"providers", "anthropic", "models"},
		appendEntry{ID: "claude-sonnet-5", Name: "Claude Sonnet 5"})

	for _, want := range []string{"# repo eval config", "# keep", "# pinned", "claude-sonnet-4-6", "id: claude-sonnet-5", "name: Claude Sonnet 5"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// The appended entry lands in the existing list, not a duplicate key.
	if strings.Count(out, "providers:") != 1 || strings.Count(out, "models:") != 2 {
		t.Errorf("unexpected structure:\n%s", out)
	}
}

func TestAppendToListYAMLCreatesPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".evolve.yaml")
	in := "layout: single\n"
	out := writeAppend(t, path, in, []string{"providers", "openai", "models"}, appendEntry{ID: "gpt-6"})
	var cfg struct {
		Layout    string `json:"layout"`
		Providers map[string]struct {
			Models []appendEntry `json:"models"`
		} `json:"providers"`
	}
	if err := DecodeFile(path, &cfg); err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if cfg.Layout != "single" {
		t.Errorf("layout = %q, want single (existing keys must survive):\n%s", cfg.Layout, out)
	}
	if got := cfg.Providers["openai"].Models; len(got) != 1 || got[0].ID != "gpt-6" {
		t.Errorf("openai models = %v, want [gpt-6]:\n%s", got, out)
	}
}

func TestAppendToListYAMLMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".evolve.yaml")
	writeAppend(t, path, "", []string{"providers", "anthropic", "models"}, appendEntry{ID: "m"})
	var cfg struct {
		Providers map[string]struct {
			Models []appendEntry `json:"models"`
		} `json:"providers"`
	}
	if err := DecodeFile(path, &cfg); err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if got := cfg.Providers["anthropic"].Models; len(got) != 1 || got[0].ID != "m" {
		t.Errorf("models = %v, want [m]", got)
	}
}

func TestAppendToListJSONCPreservesComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".evolve.jsonc")
	in := "{\n  // eval config\n  \"providers\": {\n    \"anthropic\": {\n      \"models\": [\n        {\"id\": \"claude-sonnet-4-6\"}, // pinned\n      ],\n    },\n  },\n}\n"
	out := writeAppend(t, path, in, []string{"providers", "anthropic", "models"},
		appendEntry{ID: "claude-sonnet-5", Name: "Claude Sonnet 5"})
	for _, want := range []string{"// eval config", "// pinned", "claude-sonnet-4-6", "claude-sonnet-5"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	var cfg struct {
		Providers map[string]struct {
			Models []appendEntry `json:"models"`
		} `json:"providers"`
	}
	if err := DecodeFile(path, &cfg); err != nil {
		t.Fatalf("DecodeFile after append: %v", err)
	}
	if got := cfg.Providers["anthropic"].Models; len(got) != 2 || got[1].ID != "claude-sonnet-5" {
		t.Errorf("models = %v, want the appended entry last", got)
	}
}

func TestAppendToListJSONCreatesNestedPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".evolve.json")
	out := writeAppend(t, path, "{\"layout\": \"single\"}\n", []string{"providers", "google", "models"},
		appendEntry{ID: "gemini-4"})
	var cfg struct {
		Layout    string `json:"layout"`
		Providers map[string]struct {
			Models []appendEntry `json:"models"`
		} `json:"providers"`
	}
	if err := DecodeFile(path, &cfg); err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if cfg.Layout != "single" || len(cfg.Providers["google"].Models) != 1 {
		t.Errorf("unexpected result:\n%s", out)
	}
}

func TestAppendToListRejectsNonList(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".evolve.yaml")
	if err := os.WriteFile(path, []byte("providers:\n  anthropic:\n    models: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendToList(path, []string{"providers", "anthropic", "models"}, []any{appendEntry{ID: "m"}}); err == nil {
		t.Error("want error when the target is not a list")
	}
}

func TestAppendToListRejectsUnsupportedExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".evolve.toml")
	if err := AppendToList(path, []string{"a"}, []any{appendEntry{ID: "m"}}); err == nil {
		t.Error("want error for unsupported extension")
	}
}
