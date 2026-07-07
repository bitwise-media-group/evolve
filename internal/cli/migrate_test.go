// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"

	"github.com/bitwise-media-group/evolve/internal/results"
)

// v4Repo writes a minimal single-skill repo whose results file is the pre-v5
// (tier-major) structural shape, and returns the repo root and results dir.
func v4Repo(t *testing.T) (root, resultsDir string) {
	t.Helper()
	root = t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude-plugin/plugin.json", `{"name":"solo","version":"0.1.0"}`)
	write("skills/solo-skill/SKILL.md", "---\nname: solo-skill\n---\nx\n")
	// EvalSets only surfaces a skill that has a triggers/evals spec file.
	write("evals/solo-skill/triggers.json", `{"skill_name":"solo-skill","triggers":[]}`)
	write("evals/solo-skill/results.json", `{
  "schema": 4, "plugin": "solo", "skill": "solo-skill",
  "triggers": {
    "anthropic/claude-haiku-4-5": {
      "provider": "anthropic", "model": "claude-haiku-4-5", "executed": true,
      "ran_at": "2026-06-10T00:00:00Z",
      "results": [{"query": "q1", "should_trigger": true, "hits": 2, "runs": 3, "passed": true}],
      "summary": {"passed": 1, "total": 1}
    }
  }
}`)
	return root, filepath.Join(root, "evals", "solo-skill")
}

func TestMigrateResults(t *testing.T) {
	root, resultsDir := v4Repo(t)
	o := &Options{Viper: viper.New(), Root: root, Layout: "auto", ResultsFormat: "json"}

	upgraded, err := o.MigrateResults()
	if err != nil {
		t.Fatal(err)
	}
	if len(upgraded) != 1 {
		t.Fatalf("upgraded = %+v, want exactly one outcome", upgraded)
	}
	if got := upgraded[0]; got.Plugin != "solo" || got.Skill != "solo-skill" || got.FromSchema != 4 {
		t.Errorf("outcome = %+v, want solo/solo-skill from schema 4", got)
	}

	// The file is now current, so a second pass reports nothing to do.
	loaded, _, _ := results.LoadDir(resultsDir, "solo", "solo-skill")
	if loaded.Schema != results.Schema {
		t.Errorf("on-disk schema = %d, want %d", loaded.Schema, results.Schema)
	}
	if again, err := o.MigrateResults(); err != nil || len(again) != 0 {
		t.Fatalf("second pass = (%+v, %v), want no-op", again, err)
	}
}

func TestMigrateResultsPreservesUnmigratable(t *testing.T) {
	root, resultsDir := v4Repo(t)
	const newer = `{"schema": 99, "models": {"m": {}}}`
	path := filepath.Join(resultsDir, "results.json")
	if err := os.WriteFile(path, []byte(newer), 0o644); err != nil {
		t.Fatal(err)
	}
	o := &Options{Viper: viper.New(), Root: root, Layout: "auto", ResultsFormat: "json"}

	if _, err := o.MigrateResults(); err == nil {
		t.Fatal("a newer-than-current schema must error, not silently reset")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != newer {
		t.Error("MigrateResults must leave an unmigratable file untouched")
	}
}
