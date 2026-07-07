// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/runner"
	"github.com/bitwise-media-group/evolve/internal/tokencount"
)

// fakeHarness is a test harness that also yields the canonical model the
// selection runs, and optionally the vendor token counter (model.TokenCounter).
type fakeHarness interface {
	harness.Harness
	canonicalModel() model.Model
}

// fakeTriggerProvider is a harness that triggers on queries containing "please
// trigger". Its CLI is `sh`, so harness.Available always finds it on PATH.
type fakeTriggerProvider struct {
	priced bool
}

func (f *fakeTriggerProvider) ID() string          { return "fake" }
func (f *fakeTriggerProvider) Name() string        { return "Fake" }
func (f *fakeTriggerProvider) CLI() []string       { return []string{"sh"} } // always on PATH
func (f *fakeTriggerProvider) EnvKeys() []string   { return []string{"FAKE_KEY"} }
func (f *fakeTriggerProvider) SkillDirs() []string { return []string{filepath.Join(".fake", "skills")} }
func (f *fakeTriggerProvider) canonicalModel() model.Model {
	m := model.Model{
		ID: "fake/model-1", ProviderID: "fake", Name: "Fake Model 1",
		Supported: map[string]string{"fake": "model-1"}, Preferred: "fake",
	}
	if f.priced {
		in, out := 2.0, 10.0
		m.InputUSD, m.OutputUSD = &in, &out
	}
	return m
}
func (f *fakeTriggerProvider) TriggerSpec(ws, query, cliModelID string, _ bool) model.CommandSpec {
	return model.CommandSpec{Argv: []string{"fake-cli", query}, Dir: ws}
}
func (f *fakeTriggerProvider) ScanLine(line []byte, skill string) (bool, string) {
	return bytes.Contains(line, []byte("ACTIVATE:"+skill)), ""
}

// countingTriggerProvider adds the vendor token-counting capability.
type countingTriggerProvider struct{ fakeTriggerProvider }

func (c *countingTriggerProvider) CountTokens(_ context.Context, _, text string) (int, error) {
	return len(text), nil
}

// failCountProvider is counting-capable but its counting API always fails, so a
// run records execution results without token counts — the unfillable-count case.
type failCountProvider struct{ fakeTriggerProvider }

func (*failCountProvider) CountTokens(context.Context, string, string) (int, error) {
	return 0, errors.New("counting unavailable")
}

// fakeCounterFor exposes a fake harness's optional CountTokens through
// run.Options.CounterFor, scoped to its own provider id. Returns nil when the
// fake has no counting capability (so run falls back to no counts).
func fakeCounterFor(h fakeHarness) func(string) (model.TokenCounter, bool) {
	tc, ok := any(h).(model.TokenCounter)
	if !ok {
		return nil
	}
	pid := h.canonicalModel().ProviderID
	return func(providerID string) (model.TokenCounter, bool) {
		if providerID == pid {
			return tc, true
		}
		return nil, false
	}
}

// fakeTriggerRunner emits an activation line for queries containing "please
// trigger".
type fakeTriggerRunner struct{}

func (fakeTriggerRunner) Run(_ context.Context, spec model.CommandSpec, _ time.Duration, onLine func([]byte) bool) (runner.Result, error) {
	query := spec.Argv[1]
	line := "noise"
	if strings.Contains(query, "please trigger") {
		line = `ACTIVATE:solo-skill`
	}
	hit := onLine([]byte(line))
	return runner.Result{Hit: hit, Elapsed: 1500 * time.Millisecond}, nil
}

// triggerRepoFixture builds a temp single-plugin repo with one skill and
// triggers.
func triggerRepoFixture(t *testing.T) *layout.Repo {
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
	write("skills/solo-skill/SKILL.md", "---\nname: solo-skill\ndescription: x. Use when testing.\nlicense: MIT\n---\nbody\n")
	write("evals/solo-skill/triggers.json", `{"triggers": [
		{"query": "please trigger this", "should_trigger": true},
		{"query": "unrelated request", "should_trigger": false}
	]}`)
	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func triggerOptions(t *testing.T, repo *layout.Repo, p fakeHarness) TriggerOptions {
	t.Helper()
	sel := harness.Selection{Model: p.canonicalModel(), Harness: p}
	return TriggerOptions{
		Options: Options{
			Repo:        repo,
			Selected:    []harness.Selection{sel},
			Counter:     tokencount.New(filepath.Join(t.TempDir(), "c.json"), os.Stderr),
			CounterFor:  fakeCounterFor(p),
			Runner:      fakeTriggerRunner{},
			Timeout:     30 * time.Second,
			Jobs:        2,
			ToolVersion: "test",
			Now:         func() time.Time { return time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC) },
			Stdout:      os.Stderr,
			Stderr:      os.Stderr,
		},
		Runs: 3,
	}
}

func TestTriggersWritesResults(t *testing.T) {
	repo := triggerRepoFixture(t)
	opts := triggerOptions(t, repo, &countingTriggerProvider{fakeTriggerProvider{priced: true}})
	var stdout bytes.Buffer
	opts.Stdout = &stdout

	failed, err := Triggers(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if failed {
		t.Errorf("failed = true, want all passing\n%s", stdout.String())
	}

	path := filepath.Join(repo.Root, "evals", "solo-skill", "results.json")
	file, _, _ := results.LoadDir(filepath.Dir(path), "solo", "solo-skill")
	entry := file.Trigger("fake/model-1")
	if entry == nil {
		t.Fatalf("no fake/model-1 entry; stdout:\n%s", stdout.String())
	}
	if !entry.Executed || entry.RunsPerQuery != 3 || entry.RanAt != "2026-06-11T12:00:00Z" {
		t.Errorf("header = %+v", entry.Header)
	}
	if len(entry.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(entry.Results))
	}
	r0 := entry.Results[0]
	if *r0.Hits != 3 || *r0.Runs != 3 || !*r0.Passed {
		t.Errorf("triggering query = %+v", r0)
	}
	r1 := entry.Results[1]
	if *r1.Hits != 0 || !*r1.Passed {
		t.Errorf("non-triggering query = %+v", r1)
	}
	if r0.Estimate == nil || r0.Estimate.InputCostUSD == nil {
		t.Errorf("estimate = %+v, want tokens and cost for a priced counting provider", r0.Estimate)
	}
	if *entry.Summary.Passed != 2 || entry.Summary.Total != 2 || entry.Summary.Estimate == nil {
		t.Errorf("summary = %+v", entry.Summary)
	}
	if entry.Pricing == nil {
		t.Error("priced model must snapshot pricing")
	}
}

func TestTriggersWithoutCountingCapability(t *testing.T) {
	repo := triggerRepoFixture(t)
	opts := triggerOptions(t, repo, &fakeTriggerProvider{}) // cursor-like: no counting, no pricing

	if _, err := Triggers(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	file, _, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	entry := file.Trigger("fake/model-1")
	if entry.Pricing != nil {
		t.Error("unpriced model must serialize pricing: null")
	}
	for _, r := range entry.Results {
		if r.Estimate != nil {
			t.Errorf("estimate = %+v, want absent without a counting API", r.Estimate)
		}
	}
	if entry.Summary.Estimate != nil {
		t.Error("summary estimate must be absent too")
	}
	// And the raw bytes carry an explicit pricing null.
	data, _ := os.ReadFile(filepath.Join(repo.Root, "evals", "solo-skill", "results.json"))
	if !bytes.Contains(data, []byte(`"pricing": null`)) {
		t.Error("missing explicit pricing null")
	}
}

// TestTriggersHonorEvalModels: triggers inherit the sibling evals file's models
// restriction, so a model outside it runs no queries at all.
func TestTriggersHonorEvalModels(t *testing.T) {
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
	write("skills/solo-skill/SKILL.md", "---\nname: solo-skill\ndescription: x. Use when testing.\nlicense: MIT\n---\nbody\n")
	write("evals/solo-skill/triggers.json", `{"triggers": [
		{"query": "please trigger this", "should_trigger": true}
	]}`)
	// The sibling evals file restricts the skill to anthropic; the fake/model-1
	// sweep is outside that set, so it must run no triggers.
	write("evals/solo-skill/evals.json", `{"models": ["anthropic"], "evals": [
		{"id": "e1", "prompt": "p", "assertions": ["x"]}
	]}`)
	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}

	opts := triggerOptions(t, repo, &fakeTriggerProvider{})
	if _, err := Triggers(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	file, _, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	if entry := file.Trigger("fake/model-1"); entry != nil {
		t.Errorf("fake/model-1 is outside the eval-set models; want no trigger entry, got %+v", entry)
	}
}

func TestTriggersDetectsFailures(t *testing.T) {
	repo := triggerRepoFixture(t)
	// Overwrite triggers: expect a trigger on a query the fake never triggers.
	path := filepath.Join(repo.Root, "evals", "solo-skill", "triggers.json")
	os.WriteFile(path, []byte(`{"triggers": [{"query": "never fires", "should_trigger": true}]}`), 0o644)

	opts := triggerOptions(t, repo, &countingTriggerProvider{})
	failed, err := Triggers(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !failed {
		t.Error("failed = false, want true")
	}
	file, _, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	if r := file.Trigger("fake/model-1").Results[0]; *r.Passed {
		t.Errorf("result = %+v, want failed", r)
	}
}

func TestTriggersNewSkipsCompleteEntries(t *testing.T) {
	repo := triggerRepoFixture(t)
	opts := triggerOptions(t, repo, &countingTriggerProvider{fakeTriggerProvider{priced: true}})
	if _, err := Triggers(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	opts.Stdout = &stdout
	opts.New = true
	if _, err := Triggers(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "skip: results complete") {
		t.Errorf("second --new run did not skip:\n%s", stdout.String())
	}
}

func TestTriggersNewRerunsAfterEvalChange(t *testing.T) {
	repo := triggerRepoFixture(t)
	opts := triggerOptions(t, repo, &countingTriggerProvider{fakeTriggerProvider{priced: true}})
	if _, err := Triggers(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	// Add a new query: --new must rerun.
	path := filepath.Join(repo.Root, "evals", "solo-skill", "triggers.json")
	data, _ := os.ReadFile(path)
	var spec map[string]any
	json.Unmarshal(data, &spec)
	spec["triggers"] = append(spec["triggers"].([]any),
		map[string]any{"query": "brand new please trigger", "should_trigger": true})
	updated, _ := json.Marshal(spec)
	os.WriteFile(path, updated, 0o644)

	var stdout bytes.Buffer
	opts.Stdout = &stdout
	opts.New = true
	if _, err := Triggers(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "skip:") {
		t.Errorf("--new skipped despite a new query:\n%s", stdout.String())
	}
	file, _, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	if got := len(file.Trigger("fake/model-1").Results); got != 3 {
		t.Errorf("results = %d, want 3 after the added query", got)
	}
}

func TestTriggersFailedRerunsFailingUnit(t *testing.T) {
	repo := triggerRepoFixture(t)
	// A query the fake never triggers though it should: the unit fails.
	path := filepath.Join(repo.Root, "evals", "solo-skill", "triggers.json")
	os.WriteFile(path, []byte(`{"triggers": [{"query": "never fires", "should_trigger": true}]}`), 0o644)

	opts := triggerOptions(t, repo, &countingTriggerProvider{fakeTriggerProvider{priced: true}})
	opts.Stdout = io.Discard
	if failed, err := Triggers(context.Background(), opts); err != nil || !failed {
		t.Fatalf("first run failed=%v err=%v, want a failing unit", failed, err)
	}

	var stdout bytes.Buffer
	opts.Stdout = &stdout
	opts.Failed = true
	if _, err := Triggers(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "skip:") {
		t.Errorf("--failed skipped a unit with a failing query:\n%s", stdout.String())
	}
}

func TestTriggersFailedSkipsPassingIgnoresMissing(t *testing.T) {
	repo := triggerRepoFixture(t) // default fixture: every applicable query passes
	opts := triggerOptions(t, repo, &countingTriggerProvider{fakeTriggerProvider{priced: true}})
	opts.Stdout = io.Discard
	if failed, err := Triggers(context.Background(), opts); err != nil || failed {
		t.Fatalf("first run failed=%v err=%v, want all passing", failed, err)
	}

	// --failed alone skips an all-passing unit.
	var stdout bytes.Buffer
	opts.Stdout = &stdout
	opts.Failed = true
	if _, err := Triggers(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "skip: results complete") {
		t.Errorf("--failed did not skip an all-passing unit:\n%s", stdout.String())
	}

	// Add a never-run query: --failed alone still skips (missing is not a failure);
	// --new --failed reruns the unit to fill it.
	path := filepath.Join(repo.Root, "evals", "solo-skill", "triggers.json")
	data, _ := os.ReadFile(path)
	var spec map[string]any
	json.Unmarshal(data, &spec)
	spec["triggers"] = append(spec["triggers"].([]any),
		map[string]any{"query": "unrelated and new", "should_trigger": false})
	updated, _ := json.Marshal(spec)
	os.WriteFile(path, updated, 0o644)

	stdout.Reset()
	if _, err := Triggers(context.Background(), opts); err != nil { // --failed only
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "skip: results complete") {
		t.Errorf("--failed alone must ignore merely-missing data:\n%s", stdout.String())
	}

	stdout.Reset()
	opts.New = true // --new --failed
	if _, err := Triggers(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "skip:") {
		t.Errorf("--new --failed must rerun a unit with missing data:\n%s", stdout.String())
	}
}

func TestTriggersNewMergesAndPrunes(t *testing.T) {
	repo := triggerRepoFixture(t) // "please trigger this", "unrelated request", (skip me on fake)
	opts := triggerOptions(t, repo, &countingTriggerProvider{fakeTriggerProvider{priced: true}})
	opts.Stdout = io.Discard
	if _, err := Triggers(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	// Drop "unrelated request" and add a brand-new "fresh query". Under --new only
	// the new query is a gap: the merge must keep "please trigger this", add the
	// new one, and prune the removed query.
	path := filepath.Join(repo.Root, "evals", "solo-skill", "triggers.json")
	os.WriteFile(path, []byte(`{"triggers": [
		{"query": "please trigger this", "should_trigger": true},
		{"query": "fresh query", "should_trigger": false}
	]}`), 0o644)

	opts.New = true
	if _, err := Triggers(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	file, _, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	queries := map[string]bool{}
	for _, r := range file.Trigger("fake/model-1").Results {
		queries[r.Query] = true
	}
	if !queries["please trigger this"] {
		t.Error("merge dropped a complete query the rerun did not touch")
	}
	if !queries["fresh query"] {
		t.Error("the new query was not run")
	}
	if queries["unrelated request"] {
		t.Error("a query removed from the spec was not pruned")
	}
	if len(queries) != 2 {
		t.Errorf("results = %d queries, want 2", len(queries))
	}
}

func TestNeedsNewSkipsUnfillableCounts(t *testing.T) {
	repo := triggerRepoFixture(t)
	p := &failCountProvider{} // counting-capable, unpriced, but counting fails
	topts := triggerOptions(t, repo, p)
	topts.Stdout = io.Discard
	topts.Stderr = io.Discard

	// A first run executes but cannot produce token counts.
	if _, err := Triggers(context.Background(), topts); err != nil {
		t.Fatal(err)
	}
	file, _, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	entry := file.Trigger(topts.Selected[0].Key())
	if entry == nil || !entry.Executed {
		t.Fatalf("entry = %+v, want an executed entry", entry)
	}
	if entry.Results[0].Estimate != nil {
		t.Fatalf("estimate = %+v, want nil (counting failed)", entry.Results[0].Estimate)
	}

	// --new must NOT pre-select this case: its execution data is complete and a
	// missing token-count estimate is never a selection reason.
	cat, err := Catalog(topts.Options)
	if err != nil {
		t.Fatal(err)
	}
	withNew := topts.Options
	withNew.New = true
	key := topts.Selected[0].Key()
	cr := plan.CaseRef{Skill: "solo-skill", Kind: plan.KindTriggers, Case: "please trigger this"}
	n, notes := Needs(withNew, cat, topts.Selected, plan.Tiers{Triggers: true}, "")
	if n[key][cr] {
		t.Error("--new pre-selected a case whose only missing data is a token-count estimate")
	}
	if notes[cr] != "" {
		t.Errorf("note = %q, want empty (complete case)", notes[cr])
	}
}

func TestTriggersCountOnly(t *testing.T) {
	repo := triggerRepoFixture(t)
	opts := triggerOptions(t, repo, &countingTriggerProvider{fakeTriggerProvider{priced: true}})
	opts.CountOnly = true
	opts.Runner = nil // must not be touched

	if _, err := Triggers(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	file, _, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	entry := file.Trigger("fake/model-1")
	if entry.Executed || entry.RunsPerQuery != 0 {
		t.Errorf("count-only entry = %+v, want executed=false", entry.Header)
	}
	if entry.Results[0].Estimate == nil || entry.Results[0].Passed != nil {
		t.Errorf("count-only result = %+v", entry.Results[0])
	}
}

// TestTriggersRefusesNewerSchema pins the forward-only guarantee at the engine
// level: a results file written by a newer evolve fails the run before any
// agent is spawned and survives byte-identical on disk.
func TestTriggersRefusesNewerSchema(t *testing.T) {
	repo := triggerRepoFixture(t)
	path, content := seedNewerSchema(t, repo.Root, "solo-skill")
	opts := triggerOptions(t, repo, &fakeTriggerProvider{})
	rec := &recordingRunner{inner: fakeTriggerRunner{}}
	opts.Runner = rec

	if _, err := Triggers(context.Background(), opts); !errors.Is(err, results.ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}
	if rec.calls != 0 {
		t.Errorf("runner invoked %d times, want 0 (refuse before spawning agents)", rec.calls)
	}
	if after, _ := os.ReadFile(path); string(after) != string(content) {
		t.Error("newer-schema results file must survive byte-identical")
	}
}
