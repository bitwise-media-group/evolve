// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/provider"
)

// planRepoFixture builds a single-plugin repo whose one skill has both triggers
// and evals, plus a SKILL.md with a title and description.
func planRepoFixture(t *testing.T) *layout.Repo {
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
	write("skills/solo-skill/SKILL.md", "---\ntitle: Solo Skill\ndescription: Does a thing.\n---\nbody\n")
	write("evals/solo-skill/triggers.json", `{"triggers": [
		{"query": "q1", "should_trigger": true},
		{"query": "q2", "should_trigger": false}
	]}`)
	write("evals/solo-skill/evals.json", `{"evals": [
		{"id": "e1", "prompt": "p", "assertions": [{"type": "regex", "pattern": "x"}]},
		{"id": "e2", "prompt": "p", "assertions": [{"type": "regex", "pattern": "y"}]}
	]}`)
	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestCatalogLoadsSpecsAndMetadata(t *testing.T) {
	repo := planRepoFixture(t)
	cat, err := Catalog(Options{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(cat) != 1 {
		t.Fatalf("catalog = %d skills, want 1", len(cat))
	}
	sc := cat[0]
	if sc.Skill != "solo-skill" || sc.Title != "Solo Skill" || sc.Description != "Does a thing." {
		t.Errorf("metadata = %+v", sc)
	}
	if len(sc.Triggers) != 2 || len(sc.Evals) != 2 {
		t.Errorf("specs = %d triggers, %d evals; want 2/2", len(sc.Triggers), len(sc.Evals))
	}
	if sc.ResultsDir == "" {
		t.Error("ResultsDir not set")
	}
}

func TestPlanEnumeratesUnits(t *testing.T) {
	repo := planRepoFixture(t)
	cat, err := Catalog(Options{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeTriggerProvider{}
	sels := []provider.Selection{{Provider: p, Model: p.Models()[0]}}

	both := Plan(cat, sels, nil, Tiers{Triggers: true, Evals: true})
	if len(both) != 2 {
		t.Fatalf("plan = %d units, want 2 (one triggers, one evals)", len(both))
	}

	onlyTriggers := Plan(cat, sels, nil, Tiers{Triggers: true})
	if len(onlyTriggers) != 1 || onlyTriggers[0].Kind != KindTriggers {
		t.Errorf("triggers-only plan = %+v", onlyTriggers)
	}
}

func TestFilterInclusion(t *testing.T) {
	var nilF *Filter
	if !nilF.skillIncluded("x") || !nilF.triggerIncluded("x", "q") || !nilF.evalIncluded("x", "e") {
		t.Error("nil filter must include everything")
	}

	f := &Filter{
		Skills:   map[string]bool{"a": true},
		Triggers: map[string]map[string]bool{"a": {"q1": true}},
		Evals:    map[string]map[string]bool{"a": {}}, // present but empty = none
	}
	if !f.skillIncluded("a") || f.skillIncluded("b") {
		t.Error("skillIncluded")
	}
	if !f.triggerIncluded("a", "q1") || f.triggerIncluded("a", "q2") {
		t.Error("triggerIncluded for restricted skill")
	}
	if !f.triggerIncluded("z", "anything") {
		t.Error("triggerIncluded for a skill with no entry must be unrestricted")
	}
	if f.evalIncluded("a", "e1") {
		t.Error("an empty (non-nil) eval set must include nothing")
	}
}

func TestApplicableHonorsFilter(t *testing.T) {
	repo := planRepoFixture(t)
	cat, err := Catalog(Options{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	sc := cat[0]

	f := &Filter{
		Skills:   map[string]bool{"solo-skill": true},
		Triggers: map[string]map[string]bool{"solo-skill": {"q1": true}},
		Evals:    map[string]map[string]bool{"solo-skill": {"e2": true}},
	}
	tr := applicableTriggers(sc.Triggers, "fake", "solo-skill", f)
	if len(tr) != 1 || tr[0].Query != "q1" {
		t.Errorf("triggers = %+v, want only q1", tr)
	}
	ev := applicableEvals(sc.Evals, "fake", "solo-skill", f)
	if len(ev) != 1 || ev[0].ID != "e2" {
		t.Errorf("evals = %+v, want only e2", ev)
	}

	// A skill excluded from the filter yields nothing.
	none := &Filter{Skills: map[string]bool{"other": true}}
	if got := applicableTriggers(sc.Triggers, "fake", "solo-skill", none); len(got) != 0 {
		t.Errorf("excluded skill still produced %d triggers", len(got))
	}
}

func TestNeedsDefaultsAndFlags(t *testing.T) {
	repo := planRepoFixture(t)
	cat, err := Catalog(Options{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeTriggerProvider{}
	sels := []provider.Selection{{Provider: p, Model: p.Models()[0]}}
	base := Options{Repo: repo, Selected: sels}
	key := sels[0].Key()
	tc := CaseRef{Skill: "solo-skill", Kind: KindTriggers, Case: "q1"}
	ec := CaseRef{Skill: "solo-skill", Kind: KindEvals, Case: "e1"}

	// Triggers-only default: trigger cases present, eval cases absent.
	n, notes := Needs(base, cat, sels, Tiers{Triggers: true}, "")
	if !n[key][tc] {
		t.Errorf("trigger case should need run (no --new): %+v", n[key])
	}
	if _, ok := n[key][ec]; ok {
		t.Errorf("eval case should be absent when its tier is off: %+v", n[key])
	}
	if len(notes) != 0 {
		t.Errorf("no --new/--failed: notes should be empty, got %+v", notes)
	}

	// Both default: trigger and eval cases present and needed.
	n, _ = Needs(base, cat, sels, Tiers{Triggers: true, Evals: true}, "")
	if !n[key][tc] || !n[key][ec] {
		t.Errorf("both cases should need run: %+v", n[key])
	}

	// --skill excludes other skills: matrix is empty.
	withSkill := base
	withSkill.SkillFilter = "nope"
	n, _ = Needs(withSkill, cat, sels, Tiers{Triggers: true, Evals: true}, "")
	if len(n[key]) != 0 {
		t.Errorf("skill filter should exclude solo-skill: %+v", n[key])
	}
}

func TestNeedsNewSkipsComplete(t *testing.T) {
	repo := planRepoFixture(t)
	p := &countingTriggerProvider{fakeTriggerProvider{priced: true}}
	topts := triggerOptions(t, repo, p)
	topts.Stdout = io.Discard
	topts.Stderr = io.Discard

	withNew := topts.Options
	withNew.New = true
	sels := topts.Selected
	key := sels[0].Key()
	tc := CaseRef{Skill: "solo-skill", Kind: KindTriggers, Case: "q1"}

	// Before any run, --new needs the unrun trigger (reason "new").
	cat, err := Catalog(topts.Options)
	if err != nil {
		t.Fatal(err)
	}
	if n, notes := Needs(withNew, cat, sels, Tiers{Triggers: true}, ""); !n[key][tc] {
		t.Fatalf("--new should need triggers with no stored results: %+v", n[key])
	} else if notes[tc] != ReasonNoData.String() {
		t.Errorf("note = %q, want %q", notes[tc], ReasonNoData)
	}

	// After a complete run, --new no longer needs them (q1 is complete even though
	// it failed — only --failed would reselect a failing case).
	if _, err := Triggers(context.Background(), topts); err != nil {
		t.Fatal(err)
	}
	cat, err = Catalog(topts.Options)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := Needs(withNew, cat, sels, Tiers{Triggers: true}, ""); n[key][tc] {
		t.Errorf("--new should not rerun completed triggers: %+v", n[key])
	}
}

func TestNeedsFailedSelectsFailures(t *testing.T) {
	repo := triggerRepoFixture(t) // every applicable query passes
	p := &countingTriggerProvider{fakeTriggerProvider{priced: true}}
	topts := triggerOptions(t, repo, p)
	topts.Stdout = io.Discard
	topts.Stderr = io.Discard

	withFailed := topts.Options
	withFailed.Failed = true
	sels := topts.Selected
	key := sels[0].Key()
	passing := CaseRef{Skill: "solo-skill", Kind: KindTriggers, Case: "please trigger this"}

	// After a passing run, --failed must not select the passing case.
	if _, err := Triggers(context.Background(), topts); err != nil {
		t.Fatal(err)
	}
	cat, err := Catalog(topts.Options)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := Needs(withFailed, cat, sels, Tiers{Triggers: true}, ""); n[key][passing] {
		t.Errorf("--failed should skip a passing case: %+v", n[key])
	}

	// Rewrite the spec to a single failing query, re-run, then --failed selects it
	// with reason "not passing (failed)".
	path := filepath.Join(repo.Root, "evals", "solo-skill", "triggers.json")
	os.WriteFile(path, []byte(`{"triggers": [{"query": "never fires", "should_trigger": true}]}`), 0o644)
	if _, err := Triggers(context.Background(), topts); err != nil {
		t.Fatal(err)
	}
	cat, err = Catalog(topts.Options)
	if err != nil {
		t.Fatal(err)
	}
	failing := CaseRef{Skill: "solo-skill", Kind: KindTriggers, Case: "never fires"}
	n, notes := Needs(withFailed, cat, sels, Tiers{Triggers: true}, "")
	if !n[key][failing] {
		t.Errorf("--failed should select a failing query: %+v", n[key])
	}
	if notes[failing] != ReasonNotPassing.String() {
		t.Errorf("note = %q, want %q", notes[failing], ReasonNotPassing)
	}
}
