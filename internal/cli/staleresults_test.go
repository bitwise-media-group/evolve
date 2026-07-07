// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/bitwise-media-group/evolve/internal/results"
)

func TestStaleResultsMode(t *testing.T) {
	o := &Options{Viper: viper.New()}
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().String("stale-results", "", "")

	if m := o.StaleResultsMode(cmd); m != "" {
		t.Errorf("unset mode = %q, want empty", m)
	}
	o.Viper.Set("stale_results", "drop")
	if m := o.StaleResultsMode(cmd); m != "drop" {
		t.Errorf("config mode = %q, want drop", m)
	}
	if err := cmd.Flags().Set("stale-results", "keep"); err != nil {
		t.Fatal(err)
	}
	if m := o.StaleResultsMode(cmd); m != "keep" {
		t.Errorf("flag mode = %q, want keep (flag beats config)", m)
	}
}

func TestActiveModelKeys(t *testing.T) {
	o := &Options{Viper: viper.New()}
	if _, configured, err := o.ActiveModelKeys(); err != nil || configured {
		t.Fatalf("unconfigured: configured=%v err=%v, want false/nil", configured, err)
	}

	o.Viper.Set("models", []string{"anthropic/claude-haiku-4-5"})
	keys, configured, err := o.ActiveModelKeys()
	if err != nil || !configured {
		t.Fatalf("configured: configured=%v err=%v, want true/nil", configured, err)
	}
	if len(keys) != 1 || !keys["anthropic/claude-haiku-4-5"] {
		t.Errorf("keys = %v, want only anthropic/claude-haiku-4-5", keys)
	}
}

// staleRepo writes a minimal single-skill repo whose results file holds one
// active and one non-active model, and returns the repo root.
func staleRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
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

	hdr := func(provider, model string) results.Header {
		return results.Header{
			Provider: provider, Model: model, Display: model,
			ToolVersion: "test", RanAt: "2026-06-11T10:00:00Z", TimeoutSeconds: 120,
		}
	}
	f := &results.File{Schema: results.Schema, Plugin: "solo", Skill: "solo-skill"}
	f.SetTrigger("anthropic/claude-haiku-4-5", &results.TriggerEntry{
		Header: hdr("anthropic", "claude-haiku-4-5"), Summary: results.TriggerSummary{Total: 1},
	})
	f.SetTrigger("google/gemini-3.5-flash", &results.TriggerEntry{
		Header: hdr("google", "gemini-3.5-flash"), Summary: results.TriggerSummary{Total: 1},
	})
	if _, err := f.SaveDir(filepath.Join(root, "evals", "solo-skill"), "json"); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestFindAndDropStaleResults(t *testing.T) {
	root := staleRepo(t)
	o := &Options{Viper: viper.New(), Root: root, Layout: "auto", ResultsFormat: "json"}
	active := map[string]bool{"anthropic/claude-haiku-4-5": true}

	stale, err := o.FindStaleResults(active)
	if err != nil {
		t.Fatal(err)
	}
	if got := StaleModels(stale); len(got) != 1 || got[0] != "google/gemini-3.5-flash" {
		t.Fatalf("stale models = %v, want [google/gemini-3.5-flash]", got)
	}

	if err := o.DropStaleResults(stale); err != nil {
		t.Fatal(err)
	}
	reloaded, _, _ := results.LoadDir(filepath.Join(root, "evals", "solo-skill"), "solo", "solo-skill")
	if _, ok := reloaded.Models["google/gemini-3.5-flash"]; ok {
		t.Error("dropped model still on disk")
	}
	if _, ok := reloaded.Models["anthropic/claude-haiku-4-5"]; !ok {
		t.Error("active model was wrongly dropped")
	}

	// Nothing stale once the active set covers what remains.
	if stale, err := o.FindStaleResults(active); err != nil || len(stale) != 0 {
		t.Fatalf("post-drop stale = %v err=%v, want none", stale, err)
	}
}

// TestFindStaleResultsRefusesNewerSchema pins that the stale-results sweep — a
// path that can rewrite files via DropStaleResults — propagates the
// forward-only refusal instead of treating a newer file as a blank slate.
func TestFindStaleResultsRefusesNewerSchema(t *testing.T) {
	root := staleRepo(t)
	path := filepath.Join(root, "evals", "solo-skill", "results.json")
	if err := os.WriteFile(path, []byte(`{"schema": 99, "models": {"m": {}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	o := &Options{Viper: viper.New(), Root: root, Layout: "auto", ResultsFormat: "json"}
	if _, err := o.FindStaleResults(map[string]bool{}); !errors.Is(err, results.ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}
}
