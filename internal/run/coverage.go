// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/bitwise-media-group/evolve/internal/encfmt"
	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/manifest"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/results"
)

// SkillCoverage is one skill's coverage datum: its repo-relative SKILL.md path,
// line count, and whether it is covered by a current eval result. It is the data
// the Cobertura report renders — a skill is one class, its plugin one package.
type SkillCoverage struct {
	Plugin  string // plugin name (Cobertura package)
	Skill   string // skill directory name (Cobertura class)
	SkillMD string // repo-relative SKILL.md path (Cobertura class filename)
	Lines   int    // SKILL.md line count via manifest.SplitLines
	Covered bool   // see Coverage
}

// Coverage enumerates every skill across the repository — every
// plugins/*/skills/*/SKILL.md, and single-layout skills/*, not just those with
// evals — and reports whether each is covered. A skill is covered by a "current"
// eval result: one whose stored fingerprints still match the present eval spec and
// skill content, the exact staleness predicate --modified applies. Skills with no
// evals, no results, or only stale results are reported uncovered (0%).
//
// configured is the model matrix the repo runs (provider-resolved). In the default
// mode a skill is covered when any model has a current result; under strict it is
// covered only when every model in its resolved matrix — configured ∩ the eval
// set's models restriction — has a current result, so a missing or stale model is
// a gap. configured is ignored in the default mode.
func Coverage(repo *layout.Repo, configured []model.Model, strict bool) ([]SkillCoverage, error) {
	var out []SkillCoverage
	for _, p := range repo.Plugins {
		matches, err := filepath.Glob(filepath.Join(p.SkillsDir, "*", "SKILL.md"))
		if err != nil {
			return nil, err
		}
		sort.Strings(matches)
		for _, skillMD := range matches {
			skillDir := filepath.Dir(skillMD)
			lines := 0
			if data, err := os.ReadFile(skillMD); err == nil {
				lines = len(manifest.SplitLines(string(data)))
			}
			out = append(out, SkillCoverage{
				Plugin:  p.Name,
				Skill:   filepath.Base(skillDir),
				SkillMD: repo.Rel(skillMD),
				Lines:   lines,
				Covered: skillCovered(p, filepath.Base(skillDir), skillDir, configured, strict),
			})
		}
	}
	return out, nil
}

// skillCovered reports whether the skill has at least one current eval result
// (default) or a current result for every authored eval on every model in its
// resolved matrix (strict). It reuses the same fingerprint machinery --modified
// uses: a result is current when !fingerprints.modified(r.SpecHash).
func skillCovered(p layout.Plugin, skill, skillDir string, configured []model.Model, strict bool) bool {
	resultsDir := filepath.Join(p.EvalsDir, skill)
	if results.Find(resultsDir) == "" {
		return false // no results file
	}
	evalsPath, err := encfmt.FindOne(resultsDir, "evals")
	if err != nil || evalsPath == "" {
		return false // no authored evals to be current against
	}
	ef, err := evalspec.LoadEvals(evalsPath)
	if err != nil || len(ef.Evals) == 0 {
		return false
	}
	file, _, _ := results.LoadDir(resultsDir, p.Name, skill)
	freshContent, err := skillContentHash(skillDir)
	if err != nil {
		return false
	}
	// The fresh spec fingerprint per authored eval, computed once.
	freshSpec := make([]string, len(ef.Evals))
	for i, c := range ef.Evals {
		freshSpec[i] = evalFingerprint(c)
	}
	current := func(key string, i int) bool {
		r, storedContent, ok := lookupEval(file, key, ef.Evals[i].ID)
		if !ok {
			return false
		}
		fp := fingerprints{storedContent: storedContent, freshContent: freshContent, freshSpec: freshSpec[i]}
		return !fp.modified(r.SpecHash)
	}

	if strict {
		keys := definedKeys(configured, ef.Models)
		if len(keys) == 0 {
			return false // no model is both configured and allowed here
		}
		for _, key := range keys {
			for i := range ef.Evals {
				if !current(key, i) {
					return false // a defined (model, eval) cell has no current result
				}
			}
		}
		return true
	}

	for _, key := range file.ModelKeys() {
		for i := range ef.Evals {
			if current(key, i) {
				return true
			}
		}
	}
	return false
}

// definedKeys is the resolved matrix for a skill: the configured model keys the
// eval set's models restriction allows (every configured model when the
// restriction is empty).
func definedKeys(configured []model.Model, allowed []string) []string {
	var keys []string
	for _, m := range configured {
		if len(allowed) == 0 || m.MatchedBy(allowed) {
			keys = append(keys, m.Key())
		}
	}
	return keys
}
