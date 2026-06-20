// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/runner"
	"github.com/bitwise-media-group/evolve/internal/tokencount"
)

// captureReporter records the ItemResults the engine streams so tests can assert
// on the per-item output, verdict, and retained paths.
type captureReporter struct {
	mu    sync.Mutex
	items []ItemResult
}

func (r *captureReporter) UnitStarted(UnitRef, int, int, Mode) {}
func (r *captureReporter) UnitSkipped(UnitRef, string)         {}
func (r *captureReporter) ItemStarted(UnitRef, ItemStart)      {}
func (r *captureReporter) ItemDone(_ UnitRef, item ItemResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, item)
}
func (r *captureReporter) UnitFinished(UnitRef, UnitSummary, string) {}
func (r *captureReporter) Warn(string, ...any)                       {}

// fakeEvalProvider implements Provider + EvalRunner (+ TokenCounter when
// counting). reportsUsage=false models a cursor-like provider.
type fakeEvalProvider struct {
	reportsUsage bool
	priced       bool
}

func (f *fakeEvalProvider) Name() string    { return "fake" }
func (f *fakeEvalProvider) Display() string { return "Fake" }
func (f *fakeEvalProvider) Models() []provider.Model {
	m := provider.Model{ID: "model-1", Display: "Fake Model 1"}
	if f.priced {
		in, out := 2.0, 10.0
		m.InputUSD, m.OutputUSD = &in, &out
	}
	return []provider.Model{m}
}
func (f *fakeEvalProvider) CLI() []string       { return []string{"sh"} }
func (f *fakeEvalProvider) EnvKeys() []string   { return []string{"FAKE_KEY"} }
func (f *fakeEvalProvider) SkillDirs() []string { return []string{filepath.Join(".fake", "skills")} }
func (f *fakeEvalProvider) TriggerSpec(ws, query, model string, _ bool) provider.CommandSpec {
	return provider.CommandSpec{Argv: []string{"fake-cli", query}, Dir: ws}
}
func (f *fakeEvalProvider) ScanLine([]byte, string) (bool, string) { return false, "" }
func (f *fakeEvalProvider) EvalSpec(ws string, c provider.EvalInput, model string) provider.CommandSpec {
	return provider.CommandSpec{Argv: []string{"agent-cli", "AGENT", c.Prompt}, Dir: ws}
}
func (f *fakeEvalProvider) ParseEvalOutput(stdout []byte) (string, *provider.Usage) {
	if !f.reportsUsage {
		return string(stdout), nil
	}
	in, out := 100, 10
	return string(stdout), &provider.Usage{InputTokens: &in, OutputTokens: &out}
}
func (f *fakeEvalProvider) ReportsUsage() bool { return f.reportsUsage }
func (f *fakeEvalProvider) RuntimeError(stdout []byte, _ int, _ bool) string {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return "empty CLI output"
	}
	return ""
}

type countingEvalProvider struct{ fakeEvalProvider }

func (c *countingEvalProvider) CountTokens(_ context.Context, _, text string) (int, error) {
	return len(text), nil
}

// fakeEvalRunner simulates the agent (writes a file, emits output), fakes the
// judge, and runs shell commands for real.
type fakeEvalRunner struct {
	exec       runner.Exec
	agentFails bool // when set, the agent run produces no output and exits non-zero
}

func (f *fakeEvalRunner) Run(ctx context.Context, spec provider.CommandSpec, timeout time.Duration, onLine func([]byte) bool) (runner.Result, error) {
	switch {
	case len(spec.Argv) > 1 && spec.Argv[1] == "AGENT":
		if f.agentFails {
			return runner.Result{ExitCode: 1, StderrTail: "auth error: invalid token", Elapsed: time.Second}, nil
		}
		if err := os.WriteFile(filepath.Join(spec.Dir, "created.txt"), []byte("agent artifact"), 0o644); err != nil {
			return runner.Result{}, err
		}
		return runner.Result{Stdout: []byte("TASK COMPLETE: created the file"), Elapsed: 2 * time.Second}, nil
	case spec.Argv[0] == "claude": // the judge
		return runner.Result{Stdout: []byte(`{"result": "{\"passed\": true, \"evidence\": \"verified\"}"}`)}, nil
	default:
		return f.exec.Run(ctx, spec, timeout, onLine)
	}
}

func evalRepoFixture(t *testing.T) *layout.Repo {
	t.Helper()
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
	write("skills/solo-skill/SKILL.md", "---\nname: solo-skill\ndescription: x. Use when testing.\nlicense: MIT\n---\nbody\n")
	write("evals/solo-skill/files/seed.txt", "fixture seed")
	write("evals/solo-skill/evals.json", `{"skill_name": "solo-skill", "evals": [{
		"id": "basic",
		"prompt": "create the file",
		"files": ["files/seed.txt"],
		"assertions": [
			{"type": "file_exists", "path": "created.txt"},
			{"type": "file_exists", "path": "seed.txt"},
			{"type": "regex", "pattern": "TASK COMPLETE"},
			{"type": "not_regex", "pattern": "FORBIDDEN"},
			{"type": "command", "run": "test -f created.txt"},
			{"type": "command", "run": "true", "requires": "no-such-binary-zzz"},
			{"type": "llm", "text": "the agent did the task"}
		]
	}]}`)
	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func evalOptions(t *testing.T, repo *layout.Repo, p provider.Provider) EvalOptions {
	t.Helper()
	return EvalOptions{
		Options: Options{
			Repo:        repo,
			Selected:    []provider.Selection{{Provider: p, Model: p.Models()[0]}},
			Counter:     tokencount.New(filepath.Join(t.TempDir(), "c.json"), os.Stderr),
			Runner:      &fakeEvalRunner{},
			Timeout:     30 * time.Second,
			Jobs:        2,
			ToolVersion: "test",
			Now:         func() time.Time { return time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC) },
			Stdout:      os.Stderr,
			Stderr:      os.Stderr,
		},
	}
}

func TestEvalsGradesEval(t *testing.T) {
	repo := evalRepoFixture(t)
	opts := evalOptions(t, repo, &countingEvalProvider{fakeEvalProvider{reportsUsage: true, priced: true}})
	var stdout bytes.Buffer
	opts.Stdout = &stdout

	failed, err := Evals(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if failed {
		t.Errorf("failed = true:\n%s", stdout.String())
	}

	file, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	entry := file.Evals["fake/model-1"]
	if entry == nil || !entry.Executed {
		t.Fatalf("entry = %+v", entry)
	}
	r := entry.Results[0]
	if r.Passed == nil || !*r.Passed || *r.Timing.ExecutorDurationSeconds != 2.0 {
		t.Errorf("result = %+v", r)
	}
	if len(r.Expectations) != 7 {
		t.Fatalf("expectations = %d, want 7", len(r.Expectations))
	}
	// The requires-missing command is skipped, everything else passes.
	for i, a := range r.Expectations {
		if a.Text == "" || a.Source != "assertion" {
			t.Errorf("expectations[%d] = %+v, want derived text and source", i, a)
		}
		if i == 5 {
			if a.Passed != nil || !strings.Contains(a.Evidence, "skipped") {
				t.Errorf("expectations[5] = %+v, want skipped", a)
			}
			continue
		}
		if a.Passed == nil || !*a.Passed {
			t.Errorf("expectations[%d] = %+v, want pass", i, a)
		}
	}
	// The per-eval summary uses grading.json semantics: skips excluded.
	if s := r.Summary; s == nil || s.Passed != 6 || s.Failed != 0 || s.Skipped != 1 ||
		s.Total != 7 || s.PassRate == nil || *s.PassRate != 1.0 {
		t.Errorf("grade summary = %+v", r.Summary)
	}
	// Timing carries the measured token total alongside the duration.
	if r.Timing.TotalTokens == nil || *r.Timing.TotalTokens != 110 {
		t.Errorf("timing = %+v, want total_tokens 110", r.Timing)
	}
	// Usage reported, cost computed from pricing: 100*2/1e6 + 10*10/1e6.
	if r.Measured == nil || *r.Measured.InputTokens != 100 || *r.Measured.CostUSD != 0.0003 {
		t.Errorf("measured = %+v", r.Measured)
	}
	if r.Estimate == nil || r.Estimate.InputCostUSD == nil {
		t.Errorf("estimate = %+v", r.Estimate)
	}
	if entry.Summary.Measured == nil || *entry.Summary.Passed != 1 {
		t.Errorf("summary = %+v", entry.Summary)
	}
}

func TestEvalsRetainsWorkspaceAndLog(t *testing.T) {
	repo := evalRepoFixture(t)
	opts := evalOptions(t, repo, &fakeEvalProvider{reportsUsage: true})
	root := t.TempDir()
	opts.RetainRoot = root
	rep := &captureReporter{}
	opts.Reporter = rep

	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if len(rep.items) != 1 {
		t.Fatalf("ItemDone calls = %d, want 1", len(rep.items))
	}
	it := rep.items[0]
	const want = "TASK COMPLETE: created the file"
	if it.Output != want {
		t.Errorf("Output = %q, want the parsed agent text", it.Output)
	}
	// The workspace is retained under the run-scoped root, not removed per-eval.
	if !strings.HasPrefix(it.WorkspacePath, root) {
		t.Errorf("WorkspacePath = %q, want a path under retain root %q", it.WorkspacePath, root)
	}
	if fi, err := os.Stat(it.WorkspacePath); err != nil || !fi.IsDir() {
		t.Errorf("workspace dir not retained: %v", err)
	}
	// The full raw stdout is written to the output log the TUI opens.
	if it.LogPath == "" {
		t.Fatal("LogPath empty, want the output log")
	}
	logBytes, err := os.ReadFile(it.LogPath)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	if string(logBytes) != want {
		t.Errorf("log content = %q, want the raw stdout", string(logBytes))
	}
}

// Without a retain root (the plain path) no workspace or log is surfaced.
func TestEvalsNoRetentionByDefault(t *testing.T) {
	repo := evalRepoFixture(t)
	opts := evalOptions(t, repo, &fakeEvalProvider{reportsUsage: true})
	rep := &captureReporter{}
	opts.Reporter = rep
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if len(rep.items) != 1 {
		t.Fatalf("ItemDone calls = %d, want 1", len(rep.items))
	}
	if it := rep.items[0]; it.WorkspacePath != "" || it.LogPath != "" {
		t.Errorf("paths surfaced without retention: %+v", it)
	}
}

func TestHeadLines(t *testing.T) {
	if got := headLines("a\nb\nc", 5); got != "a\nb\nc" {
		t.Errorf("short text = %q, want unchanged", got)
	}
	got := headLines(strings.Repeat("x\n", 30), 20)
	if n := strings.Count(got, "\n"); n != 20 {
		t.Errorf("capped newlines = %d, want 20 (incl. ellipsis line)", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("capped output should end with an ellipsis: %q", got)
	}
}

func TestEvalsCursorLikeProvider(t *testing.T) {
	repo := evalRepoFixture(t)
	opts := evalOptions(t, repo, &fakeEvalProvider{reportsUsage: false}) // no counting, no usage, no pricing

	failed, err := Evals(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if failed {
		t.Error("failed = true")
	}
	file, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	entry := file.Evals["fake/model-1"]
	r := entry.Results[0]
	if r.Measured != nil || r.Estimate != nil || entry.Pricing != nil {
		t.Errorf("cursor-like entry leaked usage data: %+v", r)
	}
	if r.Passed == nil || !*r.Passed {
		t.Errorf("result = %+v, want graded pass", r)
	}

	// --new must treat the entry as complete despite the absent usage.
	var stdout bytes.Buffer
	opts.Stdout = &stdout
	opts.New = true
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "skip: results complete") {
		t.Errorf("--new did not skip a complete usage-less entry:\n%s", stdout.String())
	}
}

func TestEvalsDetectsFailure(t *testing.T) {
	repo := evalRepoFixture(t)
	path := filepath.Join(repo.Root, "evals", "solo-skill", "evals.json")
	os.WriteFile(path, []byte(`{"evals": [{
		"id": "fails",
		"prompt": "create the file",
		"assertions": [{"type": "file_exists", "path": "never-created.txt"}]
	}]}`), 0o644)

	opts := evalOptions(t, repo, &countingEvalProvider{})
	failed, err := Evals(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !failed {
		t.Error("failed = false, want true")
	}
}

func TestEvalsRuntimeError(t *testing.T) {
	repo := evalRepoFixture(t)
	opts := evalOptions(t, repo, &countingEvalProvider{fakeEvalProvider{reportsUsage: true, priced: true}})
	opts.Runner = &fakeEvalRunner{agentFails: true}
	var stdout, stderr bytes.Buffer
	opts.Stdout = &stdout
	opts.Stderr = &stderr

	failed, err := Evals(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !failed {
		t.Errorf("failed = false, want true (a runtime error fails the sweep):\n%s", stdout.String())
	}
	// The per-item [ERROR] diagnostic is a failure, so it goes to stderr (with
	// the stderr tail the CLI reported); the "N errored" rollup stays on stdout.
	if out := stderr.String(); !strings.Contains(out, "[ERROR]") || !strings.Contains(out, "auth error: invalid token") {
		t.Errorf("stderr missing runtime-error diagnostics:\n%s", out)
	}
	if out := stdout.String(); !strings.Contains(out, "1 errored") {
		t.Errorf("stdout missing errored rollup:\n%s", out)
	}
	if strings.Contains(stdout.String(), "[ERROR]") {
		t.Errorf("[ERROR] detail must not appear on stdout:\n%s", stdout.String())
	}

	file, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	entry := file.Evals["fake/model-1"]
	if entry == nil {
		t.Fatal("no entry written")
	}
	r := entry.Results[0]
	if r.RuntimeError == "" || r.Passed != nil {
		t.Errorf("result = %+v, want RuntimeError set and Passed nil", r)
	}
	if len(r.Expectations) != 0 {
		t.Errorf("an errored eval must not grade assertions, got %d", len(r.Expectations))
	}
	if entry.Summary.Errored == nil || *entry.Summary.Errored != 1 {
		t.Errorf("summary.Errored = %v, want 1", entry.Summary.Errored)
	}
	if entry.Summary.Passed == nil || *entry.Summary.Passed != 0 ||
		entry.Summary.Failed == nil || *entry.Summary.Failed != 0 {
		t.Errorf("summary passed/failed = %v/%v, want 0/0", entry.Summary.Passed, entry.Summary.Failed)
	}
	if entry.Summary.PassRate != nil {
		t.Errorf("pass rate = %v, want nil (nothing graded)", entry.Summary.PassRate)
	}
}

func TestEvalsRuntimeErrorRerunUnderNew(t *testing.T) {
	repo := evalRepoFixture(t)
	opts := evalOptions(t, repo, &countingEvalProvider{fakeEvalProvider{reportsUsage: true, priced: true}})
	opts.Runner = &fakeEvalRunner{agentFails: true}

	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	// --new must re-run a prior runtime error, not treat it as a complete result.
	var stdout, stderr bytes.Buffer
	opts.Stdout = &stdout
	opts.Stderr = &stderr
	opts.New = true
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	// The re-run's [ERROR] diagnostic lands on stderr; a skip would surface on
	// stdout instead.
	if strings.Contains(stdout.String(), "skip:") || !strings.Contains(stderr.String(), "[ERROR]") {
		t.Errorf("--new skipped a prior runtime error instead of re-running:\nstdout:\n%s\nstderr:\n%s",
			stdout.String(), stderr.String())
	}
}

func TestEvalsFailedRerunsFailingUnit(t *testing.T) {
	repo := evalRepoFixture(t)
	path := filepath.Join(repo.Root, "evals", "solo-skill", "evals.json")
	os.WriteFile(path, []byte(`{"evals": [{
		"id": "fails",
		"prompt": "create the file",
		"assertions": [{"type": "file_exists", "path": "never-created.txt"}]
	}]}`), 0o644)

	opts := evalOptions(t, repo, &countingEvalProvider{fakeEvalProvider{reportsUsage: true, priced: true}})
	var first bytes.Buffer
	opts.Stdout, opts.Stderr = &first, &first
	if failed, err := Evals(context.Background(), opts); err != nil || !failed {
		t.Fatalf("first run failed=%v err=%v, want a failing eval", failed, err)
	}

	var stdout bytes.Buffer
	opts.Stdout = &stdout
	opts.Failed = true
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "skip:") {
		t.Errorf("--failed skipped an eval that failed its assertions:\n%s", stdout.String())
	}
}

func TestEvalsFailedSkipsPassingEntry(t *testing.T) {
	repo := evalRepoFixture(t)
	opts := evalOptions(t, repo, &countingEvalProvider{fakeEvalProvider{reportsUsage: true, priced: true}})
	var first bytes.Buffer
	opts.Stdout, opts.Stderr = &first, &first
	if failed, err := Evals(context.Background(), opts); err != nil || failed {
		t.Fatalf("first run failed=%v err=%v, want a pass", failed, err)
	}

	// --failed must skip a unit whose evals all passed.
	var stdout bytes.Buffer
	opts.Stdout = &stdout
	opts.Failed = true
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "skip: results complete") {
		t.Errorf("--failed did not skip a passing entry:\n%s", stdout.String())
	}
}

func TestEvalsFailedRerunsRuntimeError(t *testing.T) {
	repo := evalRepoFixture(t)
	opts := evalOptions(t, repo, &countingEvalProvider{fakeEvalProvider{reportsUsage: true, priced: true}})
	opts.Runner = &fakeEvalRunner{agentFails: true}
	var first bytes.Buffer
	opts.Stdout, opts.Stderr = &first, &first
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	// A prior runtime error is not a successful run, so --failed re-runs it.
	var stdout, stderr bytes.Buffer
	opts.Stdout, opts.Stderr = &stdout, &stderr
	opts.Failed = true
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "skip:") || !strings.Contains(stderr.String(), "[ERROR]") {
		t.Errorf("--failed skipped a prior runtime error instead of re-running:\nstdout:\n%s\nstderr:\n%s",
			stdout.String(), stderr.String())
	}
}

func TestEvalFilter(t *testing.T) {
	repo := evalRepoFixture(t)
	opts := evalOptions(t, repo, &countingEvalProvider{})
	opts.EvalFilter = "no-such-eval"
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo.Root, "evals", "solo-skill", "results.json")); err == nil {
		t.Error("filtered-out sweep must not write results")
	}
}
