// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package layout

import (
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "repos", name))
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
	if alpha.TriggersPath == "" || alpha.CasesPath != "" {
		t.Errorf("alpha-skill: TriggersPath=%q CasesPath=%q, want triggers only", alpha.TriggersPath, alpha.CasesPath)
	}
	if want := filepath.Join(alpha.Plugin.EvalsDir, "alpha-skill", "results.json"); alpha.ResultsPath != want {
		t.Errorf("ResultsPath = %s, want %s", alpha.ResultsPath, want)
	}

	beta := sets[1]
	if beta.TriggersPath != "" || beta.CasesPath == "" {
		t.Errorf("beta-skill: TriggersPath=%q CasesPath=%q, want cases only", beta.TriggersPath, beta.CasesPath)
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
