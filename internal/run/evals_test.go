// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// captureReporter records the ItemResults the engine streams so tests can assert
// on the per-item output, verdict, and retained paths.
type captureReporter struct {
	mu             sync.Mutex
	items          []ItemResult
	baselines      []ItemResult
	baselineStarts []ItemStart
}

func (r *captureReporter) UnitStarted(plan.UnitRef, int, int, plan.Mode) {}
func (r *captureReporter) UnitSkipped(plan.UnitRef, string)              {}
func (r *captureReporter) ItemStarted(plan.UnitRef, ItemStart)           {}
func (r *captureReporter) BaselineStarted(_ plan.UnitRef, item ItemStart) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.baselineStarts = append(r.baselineStarts, item)
}
func (r *captureReporter) ItemDone(_ plan.UnitRef, item ItemResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, item)
}
func (r *captureReporter) BaselineDone(_ plan.UnitRef, item ItemResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.baselines = append(r.baselines, item)
}
func (r *captureReporter) UnitFinished(plan.UnitRef, UnitSummary, string) {}
func (r *captureReporter) Warn(string, ...any)                            {}

// fakeEvalProvider is a harness implementing harness.Harness + EvalRunner
// (+ model.TokenCounter when counting). reportsUsage=false models a cursor-like
// harness that reports no usage.
type fakeEvalProvider struct {
	reportsUsage bool
	priced       bool
}

func (f *fakeEvalProvider) ID() string          { return "fake" }
func (f *fakeEvalProvider) Name() string        { return "Fake" }
func (f *fakeEvalProvider) CLI() []string       { return []string{"sh"} }
func (f *fakeEvalProvider) EnvKeys() []string   { return []string{"FAKE_KEY"} }
func (f *fakeEvalProvider) SkillDirs() []string { return []string{filepath.Join(".fake", "skills")} }
func (f *fakeEvalProvider) canonicalModel() model.Model {
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
func (f *fakeEvalProvider) TriggerSpec(ws, query, cliModelID string, _ bool) model.CommandSpec {
	return model.CommandSpec{Argv: []string{"fake-cli", query}, Dir: ws}
}
func (f *fakeEvalProvider) ScanLine([]byte, string) (bool, string) { return false, "" }
func (f *fakeEvalProvider) EvalSpec(ws string, c model.EvalInput, cliModelID string) model.CommandSpec {
	return model.CommandSpec{Argv: []string{"agent-cli", "AGENT", c.Prompt}, Dir: ws}
}
func (f *fakeEvalProvider) ParseEvalOutput(stdout []byte) (string, *model.Usage) {
	if !f.reportsUsage {
		return string(stdout), nil
	}
	in, out := 100, 10
	return string(stdout), &model.Usage{InputTokens: &in, OutputTokens: &out}
}
func (f *fakeEvalProvider) ReportsUsage() bool { return f.reportsUsage }
func (f *fakeEvalProvider) RuntimeError(stdout []byte, _ int, _ bool) string {
	if len(bytes.TrimSpace(stdout)) == 0 {
		return "empty CLI output"
	}
	return ""
}

// toolReportingProvider is a fakeEvalProvider that also implements
// harness.ToolCallReporter, returning a fixed set of calls regardless of
// stdout — enough to exercise tool_call grading and metrics population.
type toolReportingProvider struct {
	fakeEvalProvider
	calls []model.ToolCall
}

func (p *toolReportingProvider) ParseToolCalls([]byte) []model.ToolCall { return p.calls }

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

func (f *fakeEvalRunner) Run(ctx context.Context, spec model.CommandSpec, timeout time.Duration, onLine func([]byte) bool) (runner.Result, error) {
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

// evalRepoWithToolCall is evalRepoFixture pared to one file_exists plus one
// tool_call assertion, exercising the tool_call grade path end to end.
func evalRepoWithToolCall(t *testing.T) *layout.Repo {
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
	write("evals/solo-skill/evals.json", `{"skill_name": "solo-skill", "evals": [{
		"id": "basic",
		"prompt": "create the file",
		"assertions": [
			{"type": "file_exists", "path": "created.txt"},
			{"type": "tool_call", "tool": "Write"}
		]
	}]}`)
	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

// firstEvalResult loads the single committed result for the solo-skill fixture.
func firstEvalResult(t *testing.T, repo *layout.Repo) results.EvalResult {
	t.Helper()
	file, _, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	entry := file.Eval("fake/model-1")
	if entry == nil || len(entry.Results) == 0 {
		t.Fatalf("no results: %+v", entry)
	}
	return entry.Results[0]
}

func evalOptions(t *testing.T, repo *layout.Repo, p fakeHarness) EvalOptions {
	t.Helper()
	return EvalOptions{
		Options: Options{
			Repo:        repo,
			Selected:    []harness.Selection{{Model: p.canonicalModel(), Harness: p}},
			Counter:     tokencount.New(filepath.Join(t.TempDir(), "c.json"), os.Stderr),
			CounterFor:  fakeCounterFor(p),
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

	file, _, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	entry := file.Eval("fake/model-1")
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

func TestEvalsToolCallReporting(t *testing.T) {
	// A reporter harness: the tool_call assertion passes and per-tool execution
	// metrics are recorded.
	repo := evalRepoWithToolCall(t)
	rp := &toolReportingProvider{
		fakeEvalProvider: fakeEvalProvider{reportsUsage: true},
		calls: []model.ToolCall{
			{Name: "Write", Input: json.RawMessage(`{"file_path":"created.txt"}`)},
			{Name: "Write", Input: json.RawMessage(`{"file_path":"other.txt"}`)},
			{Name: "Bash", Input: json.RawMessage(`{"command":"ls"}`)},
		},
	}
	opts := evalOptions(t, repo, rp)
	var out bytes.Buffer
	opts.Stdout = &out
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	r := firstEvalResult(t, repo)
	if tc := r.Expectations[1]; tc.Passed == nil || !*tc.Passed {
		t.Errorf("tool_call expectation = %+v, want pass", tc)
	}
	if r.ExecutionMetrics == nil {
		t.Fatal("ExecutionMetrics = nil, want populated")
	}
	if r.ExecutionMetrics.ToolCalls["Write"] != 2 || r.ExecutionMetrics.ToolCalls["Bash"] != 1 {
		t.Errorf("tool counts = %+v, want Write:2 Bash:1", r.ExecutionMetrics.ToolCalls)
	}
	if r.ExecutionMetrics.TotalToolCalls == nil || *r.ExecutionMetrics.TotalToolCalls != 3 {
		t.Errorf("total = %v, want 3", r.ExecutionMetrics.TotalToolCalls)
	}

	// A non-reporter harness: the tool_call assertion skips (not fails) and no
	// execution metrics are recorded.
	repo2 := evalRepoWithToolCall(t)
	opts2 := evalOptions(t, repo2, &fakeEvalProvider{reportsUsage: true})
	opts2.Stdout = &out
	if _, err := Evals(context.Background(), opts2); err != nil {
		t.Fatal(err)
	}
	r2 := firstEvalResult(t, repo2)
	if tc := r2.Expectations[1]; tc.Passed != nil || !strings.Contains(tc.Evidence, "skipped") {
		t.Errorf("tool_call expectation = %+v, want skipped", tc)
	}
	if r2.ExecutionMetrics != nil {
		t.Errorf("ExecutionMetrics = %+v, want nil for non-reporter", r2.ExecutionMetrics)
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
	file, _, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	entry := file.Eval("fake/model-1")
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

	file, _, _ := results.LoadDir(filepath.Join(repo.Root, "evals", "solo-skill"), "solo", "solo-skill")
	entry := file.Eval("fake/model-1")
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

func TestEvalsFailedPreservesAndNarrows(t *testing.T) {
	repo := evalRepoFixture(t)
	// Two evals: "good" passes (created.txt is made), "bad" fails its assertion.
	path := filepath.Join(repo.Root, "evals", "solo-skill", "evals.json")
	os.WriteFile(path, []byte(`{"evals": [
		{"id": "good", "prompt": "create the file", "assertions": [{"type": "file_exists", "path": "created.txt"}]},
		{"id": "bad", "prompt": "create the file", "assertions": [{"type": "file_exists", "path": "never-created.txt"}]}
	]}`), 0o644)

	opts := evalOptions(t, repo, &countingEvalProvider{fakeEvalProvider{reportsUsage: true, priced: true}})
	var buf bytes.Buffer
	opts.Stdout, opts.Stderr = &buf, &buf
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	resultsDir := filepath.Join(repo.Root, "evals", "solo-skill")
	key := opts.Selected[0].Key()
	file, _, _ := results.LoadDir(resultsDir, "solo", "solo-skill")
	if got := byID(file.Eval(key).Results); got["good"].Passed == nil || !*got["good"].Passed || got["bad"].Passed == nil || *got["bad"].Passed {
		t.Fatalf("first run: want good pass / bad fail, got %+v", file.Eval(key).Results)
	}

	// Break the runner so any re-execution errors, then --failed. Only "bad" should
	// rerun (and now error); "good" must be preserved untouched, proving the rerun
	// narrowed to the failing case and merged rather than replaced the entry.
	opts.Runner = &fakeEvalRunner{agentFails: true}
	opts.Failed = true
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	file, _, _ = results.LoadDir(resultsDir, "solo", "solo-skill")
	got := byID(file.Eval(key).Results)
	if len(got) != 2 {
		t.Fatalf("merged results = %d, want 2 (both evals retained)", len(got))
	}
	if got["good"].Passed == nil || !*got["good"].Passed || got["good"].RuntimeError != "" {
		t.Errorf("good was not preserved: %+v", got["good"])
	}
	if got["bad"].RuntimeError == "" {
		t.Errorf("bad should have reran under the broken runner: %+v", got["bad"])
	}
}

// byID indexes eval results by id for assertion convenience.
func byID(rs []results.EvalResult) map[string]results.EvalResult {
	m := make(map[string]results.EvalResult, len(rs))
	for _, r := range rs {
		m[r.ID] = r
	}
	return m
}

// baselineRunner wraps fakeEvalRunner and records, per agent run, whether the
// skill was mounted in the workspace — so a test can prove the baseline runs
// without the skill present.
type baselineRunner struct {
	inner     fakeEvalRunner
	mu        sync.Mutex
	agentRuns []bool // true when the skill under test was symlinked in
}

func (r *baselineRunner) Run(ctx context.Context, spec model.CommandSpec, timeout time.Duration, onLine func([]byte) bool) (runner.Result, error) {
	if len(spec.Argv) > 1 && spec.Argv[1] == "AGENT" {
		_, err := os.Stat(filepath.Join(spec.Dir, ".fake", "skills", "solo-skill", "SKILL.md"))
		r.mu.Lock()
		r.agentRuns = append(r.agentRuns, err == nil)
		r.mu.Unlock()
	}
	return r.inner.Run(ctx, spec, timeout, onLine)
}

func TestEvalsBaseline(t *testing.T) {
	repo := evalRepoFixture(t)
	resultsDir := filepath.Join(repo.Root, "evals", "solo-skill")
	opts := evalOptions(t, repo, &fakeEvalProvider{reportsUsage: true})
	opts.Baseline = true
	rep := &captureReporter{}
	opts.Reporter = rep
	rn := &baselineRunner{}
	opts.Runner = rn

	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	// Two agent runs: the eval with the skill mounted, the baseline without it.
	with, without := 0, 0
	for _, mounted := range rn.agentRuns {
		if mounted {
			with++
		} else {
			without++
		}
	}
	if with != 1 || without != 1 {
		t.Fatalf("agent runs: with-skill=%d without-skill=%d, want 1 and 1 (%v)", with, without, rn.agentRuns)
	}
	if len(rep.baselines) != 1 {
		t.Errorf("BaselineDone calls = %d, want 1", len(rep.baselines))
	}
	file, _, _ := results.LoadDir(resultsDir, "solo", "solo-skill")
	entry := file.Eval("fake/model-1")
	if entry.Baseline == nil || byID(entry.Baseline.Results)["basic"].Fingerprint == "" {
		t.Fatalf("baseline not stored with a fingerprint: %+v", entry.Baseline)
	}
	if entry.Previous != nil {
		t.Errorf("a first run should record no previous: %+v", entry.Previous)
	}

	// Second run, unchanged: the baseline is cached (only the with-skill eval runs),
	// and the replaced run rotates into previous.
	rn2 := &baselineRunner{}
	opts.Runner = rn2
	opts.Reporter = &captureReporter{}
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if len(rn2.agentRuns) != 1 || !rn2.agentRuns[0] {
		t.Errorf("second run agent runs = %v, want one with-skill run (baseline cached)", rn2.agentRuns)
	}
	if file, _, _ = results.LoadDir(resultsDir, "solo", "solo-skill"); file.Eval("fake/model-1").Previous == nil {
		t.Error("second run should rotate the prior run into previous")
	}

	// Changing a fixture changes the eval fingerprint, so the baseline recomputes.
	if err := os.WriteFile(filepath.Join(resultsDir, "files", "seed.txt"), []byte("changed seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	rn3 := &baselineRunner{}
	opts.Runner = rn3
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if len(rn3.agentRuns) != 2 {
		t.Errorf("a fixture change should recompute the baseline: agent runs = %v, want 2", rn3.agentRuns)
	}
}

// TestEvalsBaselineInterleaved proves the baseline runs interleaved right before
// its eval's own run (not as a silent post-pass) and announces a start, so the
// dashboard can flag the row while the without-skill session executes: the agent
// runs land baseline-then-run for the eval, and a BaselineStarted event precedes
// the with-skill ItemDone.
func TestEvalsBaselineInterleaved(t *testing.T) {
	repo := evalRepoFixture(t)
	opts := evalOptions(t, repo, &fakeEvalProvider{reportsUsage: true})
	opts.Baseline = true
	rep := &captureReporter{}
	opts.Reporter = rep
	rn := &baselineRunner{}
	opts.Runner = rn

	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	// The single eval's baseline (without-skill) runs before its own run (with-skill):
	// the interleave reversed the old run-then-baseline order.
	if want := []bool{false, true}; len(rn.agentRuns) != 2 || rn.agentRuns[0] != want[0] || rn.agentRuns[1] != want[1] {
		t.Errorf("agent run order = %v, want %v (baseline before run)", rn.agentRuns, want)
	}
	// The baseline announced a start (so the row can show it running) and a finish.
	if len(rep.baselineStarts) != 1 {
		t.Errorf("BaselineStarted calls = %d, want 1", len(rep.baselineStarts))
	}
	if len(rep.baselines) != 1 {
		t.Errorf("BaselineDone calls = %d, want 1", len(rep.baselines))
	}
	if len(rep.baselineStarts) == 1 && rep.baselineStarts[0].Label != "basic" {
		t.Errorf("BaselineStarted label = %q, want %q", rep.baselineStarts[0].Label, "basic")
	}
}

// TestEvalsBaselineAdditiveWithNew proves --baseline composes with --new: when a
// prior run left the with-skill result complete but no baseline, a --new --baseline
// pass does not skip the unit as "complete" — a missing baseline is an additive gap
// that pulls the eval into the run-set, so it reruns with-skill alongside a fresh
// baseline (a contemporaneous pair for the lift delta).
func TestEvalsBaselineAdditiveWithNew(t *testing.T) {
	repo := evalRepoFixture(t)
	resultsDir := filepath.Join(repo.Root, "evals", "solo-skill")

	// First run without baselines: the with-skill result exists, no baseline does.
	opts := evalOptions(t, repo, &fakeEvalProvider{reportsUsage: true})
	opts.Baseline = false
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if file, _, _ := results.LoadDir(resultsDir, "solo", "solo-skill"); file.Eval("fake/model-1").Baseline != nil {
		t.Fatal("precondition: the first run should leave no baseline")
	}

	// --new alone would see the with-skill result as complete; --baseline adds the
	// eval back because its baseline is missing.
	rn := &baselineRunner{}
	opts.Baseline, opts.New, opts.Runner = true, true, rn
	var stdout bytes.Buffer
	opts.Stdout, opts.Stderr = &stdout, &stdout
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "skip: results complete") {
		t.Errorf("--new skipped the unit, overriding --baseline:\n%s", stdout.String())
	}
	// Two agent runs: the with-skill rerun and its baseline (one mounted the skill,
	// one did not).
	with, without := 0, 0
	for _, mounted := range rn.agentRuns {
		if mounted {
			with++
		} else {
			without++
		}
	}
	if with != 1 || without != 1 {
		t.Errorf("agent runs with-skill=%d without-skill=%d, want 1 and 1 (%v)", with, without, rn.agentRuns)
	}
	entry := mustLoadEval(t, resultsDir)
	if entry.Baseline == nil || byID(entry.Baseline.Results)["basic"].Fingerprint == "" {
		t.Errorf("--baseline did not fill the missing baseline: %+v", entry.Baseline)
	}
	// The with-skill eval reran, so the replaced run rotated into previous.
	if entry.Previous == nil {
		t.Errorf("a with-skill rerun should rotate previous, got nil")
	}
}

func mustLoadEval(t *testing.T, dir string) *results.EvalEntry {
	t.Helper()
	file, _, _ := results.LoadDir(dir, "solo", "solo-skill")
	entry := file.Eval("fake/model-1")
	if entry == nil {
		t.Fatal("no eval entry written")
	}
	return entry
}

// TestEvalsBaselineSkillChangeKeepsBaseline proves a skill-only change does not
// recompute the baseline — the baseline measures the skill's absence.
func TestEvalsBaselineSkillChangeKeepsBaseline(t *testing.T) {
	repo := evalRepoFixture(t)
	opts := evalOptions(t, repo, &fakeEvalProvider{reportsUsage: true})
	opts.Baseline = true
	opts.Runner = &baselineRunner{}
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	// Edit the skill body; the baseline depends only on the eval, so it must not run.
	if err := os.WriteFile(filepath.Join(repo.Root, "skills", "solo-skill", "SKILL.md"),
		[]byte("---\nname: solo-skill\ndescription: x. Use when testing.\nlicense: MIT\n---\nNEW BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rn := &baselineRunner{}
	opts.Runner = rn
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if len(rn.agentRuns) != 1 || !rn.agentRuns[0] {
		t.Errorf("skill change reran the baseline: agent runs = %v, want one with-skill run", rn.agentRuns)
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

// TestEvalsRefusesNewerSchema pins the forward-only guarantee at the engine
// level: a results file written by a newer evolve fails the run before any
// agent is spawned and survives byte-identical on disk.
func TestEvalsRefusesNewerSchema(t *testing.T) {
	repo := evalRepoFixture(t)
	path, content := seedNewerSchema(t, repo.Root, "solo-skill")
	opts := evalOptions(t, repo, &countingTriggerProvider{fakeTriggerProvider{priced: true}})
	rec := &recordingRunner{inner: &fakeEvalRunner{}}
	opts.Runner = rec

	if _, err := Evals(context.Background(), opts); !errors.Is(err, results.ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}
	if rec.calls != 0 {
		t.Errorf("runner invoked %d times, want 0 (refuse before spawning agents)", rec.calls)
	}
	if after, _ := os.ReadFile(path); string(after) != string(content) {
		t.Error("newer-schema results file must survive byte-identical")
	}
}
