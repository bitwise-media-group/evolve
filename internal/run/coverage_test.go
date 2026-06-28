// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/results"
)

// coverageRepo writes a single-plugin repo with one skill, its evals, and a
// results file carrying a current eval result for modelKey, and returns the repo
// and the skill directory. The stored fingerprints are computed the same way the
// engine does, so the result reads as current until a fixture changes.
func coverageRepo(t *testing.T, modelKey string) (*layout.Repo, string) {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude-plugin/plugin.json", `{"name":"solo","version":"0.1.0"}`)
	skillDir := filepath.Join(root, "skills", "solo-skill")
	write("skills/solo-skill/SKILL.md", "---\nname: solo-skill\ndescription: x. Use when testing.\n---\nbody\n")
	resultsDir := filepath.Join(root, "evals", "solo-skill")
	write("evals/solo-skill/evals.json", `{"evals": [{"id": "e1", "prompt": "p", "assertions": ["x"]}]}`)

	ef, err := evalspec.LoadEvals(filepath.Join(resultsDir, "evals.json"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := skillContentHash(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	file := &results.File{Schema: results.Schema, Plugin: "solo", Skill: "solo-skill"}
	file.SetEval(modelKey, &results.EvalEntry{
		Header:  results.Header{Provider: "anthropic", ContentHash: content},
		Results: []results.EvalResult{{ID: ef.Evals[0].ID, Passed: new(true), SpecHash: evalFingerprint(ef.Evals[0])}},
	})
	if _, err := file.SaveDir(resultsDir, "json"); err != nil {
		t.Fatal(err)
	}

	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	return repo, skillDir
}

func find(t *testing.T, cov []SkillCoverage, skill string) SkillCoverage {
	t.Helper()
	for _, c := range cov {
		if c.Skill == skill {
			return c
		}
	}
	t.Fatalf("skill %q absent from coverage %+v", skill, cov)
	return SkillCoverage{}
}

func TestCoverageCurrentResult(t *testing.T) {
	repo, _ := coverageRepo(t, "anthropic/m1")
	cov, err := Coverage(repo, []model.Model{{ID: "anthropic/m1", ProviderID: "anthropic"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	sc := find(t, cov, "solo-skill")
	if !sc.Covered {
		t.Error("a skill with a current eval result must be covered")
	}
	if sc.Lines == 0 || sc.SkillMD != "skills/solo-skill/SKILL.md" {
		t.Errorf("coverage datum = %+v, want lines>0 and the repo-relative SKILL.md path", sc)
	}
}

// TestCoverageStaleResult: mutating the skill content after the result was
// recorded makes the result stale, so the skill is no longer covered.
func TestCoverageStaleResult(t *testing.T) {
	repo, skillDir := coverageRepo(t, "anthropic/m1")
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: solo-skill\ndescription: changed. Use when testing.\n---\nnew body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cov, err := Coverage(repo, []model.Model{{ID: "anthropic/m1", ProviderID: "anthropic"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if find(t, cov, "solo-skill").Covered {
		t.Error("a result stale against the current skill content must not count as covered")
	}
}

// TestCoverageStrictRequiresEveryModel: strict mode treats a skill as covered
// only when every model in its resolved matrix has a current result; a configured
// model with no result fails it, where the default mode passes on one.
func TestCoverageStrictRequiresEveryModel(t *testing.T) {
	repo, _ := coverageRepo(t, "anthropic/m1")
	configured := []model.Model{
		{ID: "anthropic/m1", ProviderID: "anthropic"},
		{ID: "openai/m1", ProviderID: "openai"},
	}

	def, err := Coverage(repo, configured, false)
	if err != nil {
		t.Fatal(err)
	}
	if !find(t, def, "solo-skill").Covered {
		t.Error("default mode: one current result is enough to cover the skill")
	}

	strict, err := Coverage(repo, configured, true)
	if err != nil {
		t.Fatal(err)
	}
	if find(t, strict, "solo-skill").Covered {
		t.Error("strict mode: openai/m1 has no result, so the skill must be uncovered")
	}
}

// TestCoverageEnumeratesEvalLessSkills: a skill with no evals still appears in the
// denominator, uncovered.
func TestCoverageEnumeratesEvalLessSkills(t *testing.T) {
	root := t.TempDir()
	for rel, content := range map[string]string{
		".claude-plugin/plugin.json": `{"name":"solo","version":"0.1.0"}`,
		"skills/bare/SKILL.md":       "---\nname: bare\ndescription: x. Use when testing.\n---\nbody\n",
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	cov, err := Coverage(repo, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if sc := find(t, cov, "bare"); sc.Covered {
		t.Errorf("a skill with no evals must be uncovered, got %+v", sc)
	}
}
