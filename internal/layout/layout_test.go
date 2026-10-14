// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package layout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "e2e", "repos", name))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func pluginNames(repo *Repo) []string {
	names := make([]string, len(repo.Plugins))
	for i, p := range repo.Plugins {
		names[i] = p.Name
	}
	return names
}

func TestDetect(t *testing.T) {
	tests := []struct {
		fixture string
		want    Kind
		plugins []string
	}{
		{"marketplace", Marketplace, []string{"alpha", "beta"}},
		{"multi", Multi, []string{"gamma"}},
		{"single", Single, []string{"solo"}},                // name from the manifest, not the checkout dir
		{"plugins-no-manifests", Single, []string{"loner"}}, // plugins/ exists but holds no plugins
		{"broken", Marketplace, []string{"oops", "vers"}},   // marketplace marker wins over stray root plugin.json
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			repo, err := Detect(fixture(t, tt.fixture), Auto)
			if err != nil {
				t.Fatal(err)
			}
			if repo.Kind != tt.want {
				t.Errorf("kind = %s, want %s", repo.Kind, tt.want)
			}
			if got := pluginNames(repo); !equal(got, tt.plugins) {
				t.Errorf("plugins = %v, want %v", got, tt.plugins)
			}
		})
	}
}

func TestDetectWalkUp(t *testing.T) {
	root := fixture(t, "single")
	t.Chdir(filepath.Join(root, "skills", "solo-skill"))
	repo, err := Detect("", Auto)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, _ := filepath.EvalSymlinks(root)
	gotRoot, _ := filepath.EvalSymlinks(repo.Root)
	if gotRoot != wantRoot {
		t.Errorf("root = %s, want %s", gotRoot, wantRoot)
	}
	if repo.Kind != Single {
		t.Errorf("kind = %s, want single", repo.Kind)
	}
}

func TestDetectForced(t *testing.T) {
	// Forcing multi on a marketplace repo skips the marketplace marker.
	repo, err := Detect(fixture(t, "marketplace"), Multi)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Kind != Multi {
		t.Errorf("kind = %s, want multi", repo.Kind)
	}

	// Forcing single on a repo without a root plugin manifest fails.
	if _, err := Detect(fixture(t, "marketplace"), Single); err == nil {
		t.Error("forcing single on a marketplace repo: want error, got nil")
	}
}

func TestDetectUnrecognized(t *testing.T) {
	if _, err := Detect(t.TempDir(), Auto); err == nil {
		t.Error("empty dir: want error, got nil")
	}
}

func TestEvalSets(t *testing.T) {
	repo, err := Detect(fixture(t, "marketplace"), Auto)
	if err != nil {
		t.Fatal(err)
	}
	sets, err := repo.EvalSets()
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 2 {
		t.Fatalf("got %d eval sets, want 2", len(sets))
	}

	alpha := sets[0]
	if alpha.Plugin.Name != "alpha" || alpha.Skill != "alpha-skill" {
		t.Errorf("sets[0] = %s/%s, want alpha/alpha-skill", alpha.Plugin.Name, alpha.Skill)
	}
	if alpha.TriggersPath == "" || alpha.EvalsPath != "" {
		t.Errorf("alpha-skill: TriggersPath=%q EvalsPath=%q, want triggers only", alpha.TriggersPath, alpha.EvalsPath)
	}
	if want := filepath.Join(alpha.Plugin.EvalsDir, "alpha-skill"); alpha.ResultsDir != want {
		t.Errorf("ResultsDir = %s, want %s", alpha.ResultsDir, want)
	}

	beta := sets[1]
	if beta.TriggersPath != "" || beta.EvalsPath == "" {
		t.Errorf("beta-skill: TriggersPath=%q EvalsPath=%q, want evals only", beta.TriggersPath, beta.EvalsPath)
	}
}

// TestEvalSetsMultiFormat covers discovery of non-JSON definitions and the
// one-extension-per-stem rule.
func TestEvalSetsMultiFormat(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude-plugin/plugin.json", `{"name":"solo","version":"0.1.0"}`)
	write("evals/skill-a/evals.yaml", "evals: []\n")
	repo, err := Detect(root, Auto)
	if err != nil {
		t.Fatal(err)
	}
	sets, err := repo.EvalSets()
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 1 || filepath.Base(sets[0].EvalsPath) != "evals.yaml" {
		t.Fatalf("sets = %+v, want one yaml eval set", sets)
	}

	write("evals/skill-a/evals.json", `{"evals": []}`)
	if _, err := repo.EvalSets(); err == nil || !strings.Contains(err.Error(), "keep exactly one") {
		t.Errorf("EvalSets error = %v, want ambiguity error", err)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// writeTree creates each rel → content file under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetectAgents(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"agents/support/agent.py":         "print('hi')",
		"agents/support/SYSTEM_PROMPT.md": "Be helpful.",
		"agents/support/evals.json":       `{"agent":{"endpoints":{"local":{"url":"http://x"}}},"evals":[]}`,
		"agents/billing/evals.yaml":       "agent:\n  endpoints: {}\nevals: []\n",
		"agents/notes/README.md":          "no evals here",
	})
	repo, err := Detect(root, Auto)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Kind != Agents || !repo.IsAgentRepo() || len(repo.Plugins) != 0 {
		t.Fatalf("repo = %+v, want agents layout without plugins", repo)
	}
	if len(repo.Agents) != 2 || repo.Agents[0].Name != "billing" || repo.Agents[1].Name != "support" {
		t.Errorf("agents = %+v, want billing, support", repo.Agents)
	}
	sets, err := repo.EvalSets()
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 2 {
		t.Fatalf("sets = %+v", sets)
	}
	s := sets[1]
	wantDir := filepath.Join(root, "agents", "support")
	if !s.Agent || s.Plugin.Name != AgentsGroup || s.Plugin.Dir != root || s.Skill != "support" ||
		s.SkillDir != wantDir || s.ResultsDir != wantDir || s.TriggersPath != "" ||
		s.EvalsPath != filepath.Join(wantDir, "evals.json") {
		t.Errorf("support set = %+v", s)
	}
}

func TestDetectSingleAgent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "my-checkout")
	writeTree(t, root, map[string]string{
		"agent/agent.py":   "",
		"agent/evals.json": `{"agent":{"name":"concierge","endpoints":{}},"evals":[]}`,
	})
	repo, err := Detect(root, Auto)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Kind != Agent || len(repo.Agents) != 1 || repo.Agents[0].Name != "concierge" ||
		repo.Agents[0].Dir != filepath.Join(root, "agent") {
		t.Errorf("repo = %+v", repo)
	}
	// Without agent.name the repository root's basename names the agent.
	writeTree(t, root, map[string]string{"agent/evals.json": `{"agent":{"endpoints":{}},"evals":[]}`})
	repo, err = Detect(root, Auto)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Agents[0].Name != "my-checkout" {
		t.Errorf("name = %q, want the root basename", repo.Agents[0].Name)
	}
	t.Chdir(filepath.Join(root, "agent"))
	walked, err := Detect("", Auto)
	if err != nil {
		t.Fatal(err)
	}
	if walked.Kind != Agent {
		t.Errorf("walk-up kind = %s, want agent", walked.Kind)
	}
}

func TestDetectAgentForcedAndPrecedence(t *testing.T) {
	// A single-layout plugin keeps its subagent definitions in agents/ (no
	// evals file there): it must still detect as a plugin.
	plugin := t.TempDir()
	writeTree(t, plugin, map[string]string{
		".claude-plugin/plugin.json": `{"name":"solo","version":"0.1.0"}`,
		"agents/reviewer.md":         "---\nname: reviewer\n---\n",
		"agents/helper/notes.md":     "",
	})
	repo, err := Detect(plugin, Auto)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Kind != Single || len(repo.Agents) != 0 {
		t.Errorf("kind = %s, agents = %v; want single with no agents", repo.Kind, repo.Agents)
	}
	if _, err := Detect(plugin, Agents); err == nil {
		t.Error("forcing agents on a plugin repo: want error")
	}

	// Even with an agent marker present, the plugin marker wins under auto; a
	// forced kind consults only its own marker.
	both := t.TempDir()
	writeTree(t, both, map[string]string{
		".claude-plugin/plugin.json": `{"name":"solo","version":"0.1.0"}`,
		"agents/a/evals.json":        `{"evals":[]}`,
	})
	if repo, _ := Detect(both, Auto); repo.Kind != Single {
		t.Errorf("auto kind = %s, want single", repo.Kind)
	}
	if repo, err := Detect(both, Agents); err != nil || repo.Kind != Agents || len(repo.Agents) != 1 {
		t.Errorf("forced agents = %+v, %v", repo, err)
	}
	if k, err := ParseKind("agent"); err != nil || k != Agent {
		t.Errorf("ParseKind(agent) = %s, %v", k, err)
	}
}

func TestEvalSetsAgentAmbiguous(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"agents/a/evals.json": `{"evals":[]}`,
		"agents/a/evals.yaml": "evals: []\n",
	})
	repo, err := Detect(root, Auto)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EvalSets(); err == nil || !strings.Contains(err.Error(), "keep exactly one") {
		t.Errorf("EvalSets error = %v, want ambiguity error", err)
	}
}
