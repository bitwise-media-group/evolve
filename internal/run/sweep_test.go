// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/tokencount"
)

// recReporter records the order units start in, so a test can assert the sweep's
// execution order.
type recReporter struct {
	mu      sync.Mutex
	started []string // "skill/kind"
}

func (r *recReporter) note(u plan.UnitRef) {
	kind := "triggers"
	if u.Kind == plan.KindEvals {
		kind = "evals"
	}
	r.mu.Lock()
	r.started = append(r.started, u.Skill+"/"+kind)
	r.mu.Unlock()
}

func (r *recReporter) UnitStarted(u plan.UnitRef, _, _ int, _ plan.Mode) { r.note(u) }
func (r *recReporter) UnitSkipped(plan.UnitRef, string)                  {}
func (r *recReporter) ItemStarted(plan.UnitRef, ItemStart)               {}
func (r *recReporter) BaselineStarted(plan.UnitRef, ItemStart)           {}
func (r *recReporter) ItemDone(plan.UnitRef, ItemResult)                 {}
func (r *recReporter) BaselineDone(plan.UnitRef, ItemResult)             {}
func (r *recReporter) UnitFinished(plan.UnitRef, UnitSummary, string)    {}
func (r *recReporter) Warn(string, ...any)                               {}

// twoSkillRepo builds a repo whose two skills each have triggers and evals.
func twoSkillRepo(t *testing.T) *layout.Repo {
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
	for _, s := range []string{"a-skill", "b-skill"} {
		write("skills/"+s+"/SKILL.md", "---\nname: "+s+"\ndescription: x. Use when testing.\nlicense: MIT\n---\nbody\n")
		write("evals/"+s+"/triggers.json", `{"triggers": [{"query": "q", "should_trigger": true}]}`)
		write("evals/"+s+"/evals.json", `{"evals": [{"id": "e", "prompt": "p", "assertions": [{"type": "regex", "pattern": "x"}]}]}`)
	}
	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

// TestSweepInterleavesTiersPerSkill is the core ordering guarantee: a skill runs
// both its tiers before the next skill, rather than all triggers then all evals.
func TestSweepInterleavesTiersPerSkill(t *testing.T) {
	repo := twoSkillRepo(t)
	p := &countingTriggerProvider{fakeTriggerProvider{priced: true}}
	rep := &recReporter{}
	opts := SweepOptions{
		Options: Options{
			Repo:        repo,
			Selected:    []provider.Selection{{Provider: p, Model: p.Models()[0]}},
			Counter:     tokencount.New(filepath.Join(t.TempDir(), "c.json"), os.Stderr),
			Timeout:     30 * time.Second,
			Jobs:        2,
			CountOnly:   true, // exercise ordering without spawning agents
			ToolVersion: "test",
			Now:         func() time.Time { return time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC) },
			Reporter:    rep,
		},
		Tiers: plan.Tiers{Triggers: true, Evals: true},
		Runs:  1,
	}
	if _, err := Sweep(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	want := []string{"a-skill/triggers", "a-skill/evals", "b-skill/triggers", "b-skill/evals"}
	if len(rep.started) != len(want) {
		t.Fatalf("unit order = %v, want %v", rep.started, want)
	}
	for i, w := range want {
		if rep.started[i] != w {
			t.Fatalf("unit order = %v, want %v", rep.started, want)
		}
	}
}
