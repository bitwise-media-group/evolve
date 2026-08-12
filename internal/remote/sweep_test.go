// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import (
	"context"
	"encoding/json"
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
	"github.com/bitwise-media-group/evolve/internal/run"
)

// fakeSweepHarness satisfies harness.Harness for planning; a remote sweep
// never executes it.
type fakeSweepHarness struct{}

func (fakeSweepHarness) ID() string          { return "fake" }
func (fakeSweepHarness) Name() string        { return "Fake" }
func (fakeSweepHarness) CLI() []string       { return []string{"sh"} }
func (fakeSweepHarness) EnvKeys() []string   { return []string{"FAKE_KEY"} }
func (fakeSweepHarness) SkillDirs() []string { return []string{filepath.Join(".fake", "skills")} }
func (fakeSweepHarness) TriggerSpec(ws, query, _ string, _ bool) model.CommandSpec {
	return model.CommandSpec{Argv: []string{"fake-cli", query}, Dir: ws}
}
func (fakeSweepHarness) ScanLine([]byte, string, string) (bool, string) { return false, "" }

// fakeSweepModel can be driven by two harnesses, the preferred one first.
func fakeSweepModel() model.Model {
	return model.Model{
		ID: "fake/model-1", ProviderID: "fake", Name: "Fake Model 1",
		Supported: map[string]string{"fake": "model-1", "other": "other-model-1"},
		Preferred: "fake",
	}
}

// sweepRepo builds a single-plugin repo whose skills each carry triggers and
// evals, and returns its root alongside the detected layout.
func sweepRepo(t *testing.T, skills ...string) (*layout.Repo, string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude-plugin/plugin.json"), `{"name":"solo","version":"0.1.0"}`)
	for _, s := range skills {
		writeFile(t, filepath.Join(root, "skills", s, "SKILL.md"),
			"---\nname: "+s+"\ndescription: x. Use when testing.\nlicense: MIT\n---\nbody\n")
		writeFile(t, filepath.Join(root, "evals", s, "triggers.json"),
			`{"triggers": [{"query": "please trigger", "should_trigger": true}]}`)
		writeFile(t, filepath.Join(root, "evals", s, "evals.json"),
			`{"evals": [{"id": "e1", "prompt": "p", "assertions": [{"type": "regex", "pattern": "x"}]}]}`)
	}
	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	return repo, root
}

func sweepOptions(repo *layout.Repo, c *Client, rep run.Reporter) SweepOptions {
	return SweepOptions{
		Options: run.Options{
			Repo:     repo,
			Selected: []harness.Selection{{Model: fakeSweepModel(), Harness: fakeSweepHarness{}}},
			Reporter: rep,
			Stdout:   os.Stderr,
			Stderr:   os.Stderr,
		},
		Client:        c,
		Tiers:         plan.Tiers{Evals: true},
		Runs:          2,
		EvalTimeout:   90 * time.Second,
		Judge:         fakeSweepModel(),
		ClientVersion: "test-version",
	}
}

func TestSweepEndToEnd(t *testing.T) {
	repo, root := sweepRepo(t, "a-skill")
	resultsDir := filepath.Join(root, "evals", "a-skill")

	// Seed a prior entry, so the submitted unit carries it for in-pod seeding.
	prior, _, err := results.LoadDir(resultsDir, "solo", "a-skill")
	if err != nil {
		t.Fatal(err)
	}
	prior.SetEval("fake/model-1", &results.EvalEntry{Header: results.Header{Model: "model-1", RanAt: "2026-01-01T00:00:00Z"}})
	if _, err := prior.SaveDir(resultsDir, "json"); err != nil {
		t.Fatal(err)
	}

	// The settled unit hands back a real eval entry to land.
	pass := true
	entry, err := json.Marshal(&results.EvalEntry{
		Header:  results.Header{Model: "model-1", Executed: true, RanAt: "2026-08-12T00:00:00Z"},
		Results: []results.EvalResult{{ID: "e1", Passed: &pass}},
		Summary: results.EvalSummary{Total: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	fp := newFakePatchy(t)
	fp.settled = UnitStatusWire{
		Name: "eval-1-u000", Phase: "Complete",
		Result: &UnitResult{Tier: 2, Model: "fake/model-1", Harness: "fake",
			Summary: ResultSummary{CasesPassed: 1, Outcome: "ok"}, Entry: entry},
	}
	rep := &recorder{}
	failed, err := Sweep(context.Background(), sweepOptions(repo, newTestClient(t, fp), rep))
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if failed {
		t.Error("failed = true for an all-pass sweep")
	}

	// The submission carries one fully-rendered unit spec.
	if fp.submission == nil || fp.submission.Version != SubmissionVersion {
		t.Fatalf("submission = %+v, want version %s", fp.submission, SubmissionVersion)
	}
	if len(fp.submission.Units) != 1 {
		t.Fatalf("submitted %d units, want 1", len(fp.submission.Units))
	}
	spec := fp.submission.Units[0]
	if spec.Skill != "a-skill" || spec.Plugin != "solo" || spec.Tier != 2 || spec.Model != "fake/model-1" {
		t.Errorf("spec identity = %+v", spec)
	}
	if spec.TimeoutMS != 90_000 || spec.RunsPerQuery != 2 || spec.ClientVersion != "test-version" {
		t.Errorf("spec knobs = timeout %d runs %d version %q", spec.TimeoutMS, spec.RunsPerQuery, spec.ClientVersion)
	}
	if len(spec.Harnesses) != 2 || spec.Harnesses[0] != (HarnessOption{Harness: "fake", ModelID: "model-1"}) {
		t.Errorf("harness options = %+v, want the preferred harness first", spec.Harnesses)
	}
	if spec.Cases != nil {
		t.Errorf("cases = %v, want nil (no narrowing flags)", spec.Cases)
	}
	if spec.Judge == nil || spec.Judge.Model != "fake/model-1" || spec.Judge.ModelID != "model-1" {
		t.Errorf("judge = %+v", spec.Judge)
	}
	if len(spec.PriorEntry) == 0 {
		t.Error("the seeded prior entry never reached the spec")
	}

	// The workspace bundle was uploaded under the spec's digest.
	if spec.Workspace.Digest == "" {
		t.Fatal("spec has no workspace digest")
	}
	if _, ok := fp.blobs[spec.Workspace.Digest]; !ok {
		t.Errorf("workspace %s never uploaded (server has %d blobs)", spec.Workspace.Digest, len(fp.blobs))
	}

	// The settled entry landed in the local results file under the model key.
	f, _, err := results.LoadDir(resultsDir, "solo", "a-skill")
	if err != nil {
		t.Fatal(err)
	}
	landed := f.Eval("fake/model-1")
	if landed == nil || landed.RanAt != "2026-08-12T00:00:00Z" || len(landed.Results) != 1 {
		t.Fatalf("landed entry = %+v, want the remote result merged over the prior", landed)
	}

	// The reporter saw the unit start and finish.
	var methods []string
	for _, c := range rep.calls {
		methods = append(methods, c.Method)
	}
	var started, finished bool
	for _, c := range rep.calls {
		switch c.Method {
		case "UnitStarted":
			started = true
		case "UnitFinished":
			finished = true
			if c.Sum.Passed != 1 || c.Sum.Total != 1 || !c.Sum.Executed {
				t.Errorf("UnitFinished summary = %+v", c.Sum)
			}
		}
	}
	if !started || !finished {
		t.Errorf("reporter calls = %v, want UnitStarted and UnitFinished", methods)
	}
}

// runFailedSweep runs a one-skill sweep whose only unit settles as fp.settled
// describes, returning the strict-failure flag and the recorded reporter calls.
func runFailedSweep(t *testing.T, settled UnitStatusWire) (bool, *recorder) {
	t.Helper()
	repo, _ := sweepRepo(t, "a-skill")
	fp := newFakePatchy(t)
	fp.settled = settled
	rep := &recorder{}
	failed, err := Sweep(context.Background(), sweepOptions(repo, newTestClient(t, fp), rep))
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	return failed, rep
}

func TestSweepHarnessUnavailableSkips(t *testing.T) {
	failed, rep := runFailedSweep(t, UnitStatusWire{
		Name: "eval-1-u000", Phase: "Failed",
		Reason: "HarnessUnavailable", Detail: "no runner serves fake",
	})
	if failed {
		t.Error("an unavailable harness is a skip, not a strict failure")
	}
	var skipped bool
	for _, c := range rep.calls {
		if c.Method == "UnitSkipped" && strings.Contains(c.Msg, "no runner serves fake") {
			skipped = true
		}
	}
	if !skipped {
		t.Errorf("reporter calls = %+v, want a UnitSkipped with the server detail", rep.calls)
	}
}

func TestSweepWorkspaceLostFails(t *testing.T) {
	failed, rep := runFailedSweep(t, UnitStatusWire{
		Name: "eval-1-u000", Phase: "Failed", Reason: "WorkspaceLost",
	})
	if !failed {
		t.Error("a lost workspace must count as failed")
	}
	assertWarned(t, rep, "workspace expired")
}

func TestSweepRemoteFailureFails(t *testing.T) {
	failed, rep := runFailedSweep(t, UnitStatusWire{
		Name: "eval-1-u000", Phase: "Failed", Reason: "JobFailed", Detail: "pod OOM-killed",
	})
	if !failed {
		t.Error("a remotely-failed unit must count as failed")
	}
	assertWarned(t, rep, "pod OOM-killed")
}

func assertWarned(t *testing.T, rep *recorder, want string) {
	t.Helper()
	for _, c := range rep.calls {
		if c.Method == "Warn" && strings.Contains(c.Msg, want) {
			return
		}
	}
	t.Errorf("no warning mentioning %q in %+v", want, rep.calls)
}

func TestPlanUnitsSharesTriggerBundlePerPlugin(t *testing.T) {
	repo, _ := sweepRepo(t, "a-skill", "b-skill")
	opts := sweepOptions(repo, nil, &recorder{})
	opts.Tiers = plan.Tiers{Triggers: true}
	opts.TriggerTimeout = 45 * time.Second

	units, err := planUnits(&opts)
	if err != nil {
		t.Fatalf("planUnits: %v", err)
	}
	if len(units) != 2 {
		t.Fatalf("planned %d units, want one trigger unit per skill", len(units))
	}
	if units[0].bundle != units[1].bundle {
		t.Error("sibling trigger units must share the plugin's one bundle")
	}
	for _, u := range units {
		if u.spec.Tier != 1 || u.spec.TimeoutMS != 45_000 || u.spec.RunsPerQuery != 2 {
			t.Errorf("trigger spec = %+v", u.spec)
		}
		if u.spec.Workspace.Digest != units[0].bundle.Digest {
			t.Errorf("spec digest %s != bundle digest %s", u.spec.Workspace.Digest, units[0].bundle.Digest)
		}
	}
}

func TestRunSet(t *testing.T) {
	sc := plan.SkillCatalog{Skill: "s"}
	sel := harness.Selection{Model: fakeSweepModel(), Harness: fakeSweepHarness{}}
	ref := func(c string) plan.CaseRef { return plan.CaseRef{Skill: "s", Kind: plan.KindEvals, Case: c} }
	need := func(m map[plan.CaseRef]bool) map[string]map[plan.CaseRef]bool {
		return map[string]map[plan.CaseRef]bool{sel.Key(): m}
	}

	// No narrowing: everything needed collapses to nil (run all).
	cases, any := runSet(sc, sel, plan.KindEvals, need(map[plan.CaseRef]bool{ref("a"): true, ref("b"): true}), false, "")
	if cases != nil || !any {
		t.Errorf("unnarrowed = (%v, %v), want (nil, true)", cases, any)
	}

	// Selection flags: only the needed subset is listed.
	cases, any = runSet(sc, sel, plan.KindEvals, need(map[plan.CaseRef]bool{ref("a"): true, ref("b"): false}), true, "")
	if !any || len(cases) != 1 || cases[0] != "a" {
		t.Errorf("narrowed = (%v, %v), want ([a], true)", cases, any)
	}

	// Nothing needed: the unit is not planned at all.
	if _, any = runSet(sc, sel, plan.KindEvals, need(map[plan.CaseRef]bool{ref("a"): false}), true, ""); any {
		t.Error("any = true with nothing needed")
	}

	// An eval filter forces the explicit list even when everything is needed.
	cases, any = runSet(sc, sel, plan.KindEvals, need(map[plan.CaseRef]bool{ref("a"): true}), false, "a")
	if !any || len(cases) != 1 {
		t.Errorf("filtered = (%v, %v), want the explicit list", cases, any)
	}

	// Other skills and tiers never leak into the unit's run-set.
	other := map[plan.CaseRef]bool{
		{Skill: "other", Kind: plan.KindEvals, Case: "x"}: true,
		{Skill: "s", Kind: plan.KindTriggers, Case: "q"}:  true,
	}
	if _, any = runSet(sc, sel, plan.KindEvals, need(other), true, ""); any {
		t.Error("a foreign skill/tier case counted toward this unit")
	}
}

func TestUploadBundlesDedupes(t *testing.T) {
	fp := newFakePatchy(t)
	c := newTestClient(t, fp)
	ctx := context.Background()

	shared, err := buildBundle(nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "f.txt"), "x")
	distinct, err := buildBundle([]bundleFile{{rel: "f.txt", src: filepath.Join(dir, "f.txt"), mode: 0o644}})
	if err != nil {
		t.Fatal(err)
	}

	units := []plannedUnit{{bundle: shared}, {bundle: shared}, {bundle: distinct}}
	if err := uploadBundles(ctx, c, units); err != nil {
		t.Fatalf("uploadBundles: %v", err)
	}
	if got := fp.puts.Load(); got != 2 {
		t.Errorf("uploaded %d bundles, want 2 (the shared bundle uploads once)", got)
	}
	// A second sweep over the same bundles uploads nothing: the server has them.
	if err := uploadBundles(ctx, c, units); err != nil {
		t.Fatalf("uploadBundles(again): %v", err)
	}
	if got := fp.puts.Load(); got != 2 {
		t.Errorf("re-uploaded already-cached bundles (%d puts total)", got)
	}
}
