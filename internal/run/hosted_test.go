// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitwise-media-group/evolve/internal/agui"
	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/tokencount"
)

// hostedRepo is a one-agent repository (agents/support) with three evals: one
// that passes, one whose outcome assertion fails, and one the fake agent
// errors on.
func hostedRepo(t *testing.T) *layout.Repo {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "agents", "support")
	for rel, content := range map[string]string{
		"agent.py":         "def run(): ...\n",
		"SYSTEM_PROMPT.md": "Be helpful.\n",
		"files/order.json": `{"orderId":"1234"}`,
		"evals.json": `{
			"agent": {"endpoints": {"local": {"url": "http://localhost:8000/"}}},
			"evals": [
				{"id": "refund", "prompt": "Refund order 1234.", "files": ["files/order.json"],
				 "tools": [{"name": "confirm_refund", "description": "Ask the user"}],
				 "responses": [{"tool": "^confirm_refund$", "result": {"approved": true}}],
				 "assertions": [
					{"type": "outcome", "expect": "completed"},
					{"type": "hitl", "tool": "^confirm_refund$"},
					{"type": "tool_call", "tool": "^issue_refund$", "max": 1, "result": "ok"},
					{"type": "tool_sequence", "tools": ["confirm_refund", "issue_refund"]},
					{"type": "state", "pointer": "/refunds/0", "equals": "1234"},
					{"type": "conformance"},
					{"type": "regex", "pattern": "Refunded"},
					{"type": "command", "run": "test -f .agent/state.json && test -f order.json"},
					"The refund was confirmed first"
				 ]},
				{"id": "stalls", "prompt": "Pick a date.",
				 "assertions": [{"type": "outcome", "expect": "completed"}]},
				{"id": "broken", "prompt": "Crash.",
				 "assertions": [{"type": "outcome", "expect": "completed"}]}
			]
		}`,
	} {
		path := filepath.Join(dir, rel)
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
	return repo
}

// fakeConverse scripts the hosted agent per prompt and counts conversations.
type fakeConverse struct {
	mu      sync.Mutex
	calls   atomic.Int64
	prompts []string
	err     error
}

func (f *fakeConverse) converse(_ context.Context, ep agui.Endpoint, s agui.Script) (*agui.Transcript, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.prompts = append(f.prompts, s.Prompt)
	f.mu.Unlock()
	if f.err != nil {
		return &agui.Transcript{}, f.err
	}
	if ep.URL != "http://agent.test/" {
		return nil, fmt.Errorf("unexpected endpoint %q", ep.URL)
	}
	t := &agui.Transcript{Protocol: "ag-ui", Seconds: 1.5, State: json.RawMessage(`{}`)}
	switch s.Prompt {
	case "Refund order 1234.":
		ok, approved := "ok", `{"approved":true}`
		t.Outcome, t.Output = agui.Completed, "Refunded order 1234."
		t.Runs = make([]agui.Run, 2)
		t.State = json.RawMessage(`{"refunds":["1234"]}`)
		t.ToolCalls = []agui.ToolCall{
			{Run: 1, ID: "c1", Name: "confirm_refund", Args: `{"orderId":"1234"}`, Result: &approved, Frontend: true},
			{Run: 2, ID: "c2", Name: "issue_refund", Args: `{"orderId":"1234"}`, Result: &ok},
		}
		t.HITL = []agui.HITLRequest{{Run: 1, Kind: agui.HITLTool, ID: "c1", Name: "confirm_refund", Answered: true}}
		t.Usage = []agui.TokenUsage{{Provider: "anthropic", Model: "claude-sonnet-5",
			InputTokens: new(1000), CachedInputTokens: new(600), OutputTokens: new(100)}}
		t.Violations = []agui.Violation{{Run: 1, Event: 3, Severity: agui.SeverityWarning, Rule: agui.RuleUnknown,
			Message: `TEXT_MESSAGE_START: unknown field "x"; ignored`}}
	case "Pick a date.":
		t.Outcome, t.Output = agui.AwaitingInput, "When?"
		t.Runs = make([]agui.Run, 1)
		t.HITL = []agui.HITLRequest{{Run: 1, Kind: agui.HITLTool, ID: "c9", Name: "pick_date"}}
	default:
		t.Outcome, t.RuntimeError = agui.Errored, "HTTP 502: bad gateway"
		t.Runs = make([]agui.Run, 1)
	}
	return t, nil
}

func hostedOptions(t *testing.T, repo *layout.Repo, f *fakeConverse) EvalOptions {
	t.Helper()
	sel, ok := harness.HostedSelection("ag-ui", "local", "Local")
	if !ok {
		t.Fatal("no ag-ui harness")
	}
	return EvalOptions{
		Options: Options{
			Repo:        repo,
			Selected:    []harness.Selection{sel},
			Counter:     tokencount.New(filepath.Join(t.TempDir(), "c.json"), os.Stderr),
			Runner:      &fakeEvalRunner{},
			Timeout:     30 * time.Second,
			Jobs:        2,
			MaxTurns:    20,
			Baseline:    true, // on by default; agents must never run one
			ToolVersion: "test",
			Now:         func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) },
			Stdout:      os.Stderr,
			Stderr:      os.Stderr,
			Agents:      map[string]map[string]agui.Endpoint{"support": {"local": {URL: "http://agent.test/"}}},
			Converse:    f.converse,
		},
		Judge: passingJudge{},
	}
}

func loadHosted(t *testing.T, repo *layout.Repo) *results.EvalEntry {
	t.Helper()
	file, _, err := results.LoadDir(filepath.Join(repo.Root, "agents", "support"), layout.AgentsGroup, "support")
	if err != nil {
		t.Fatal(err)
	}
	entry := file.Eval("ag-ui/local")
	if entry == nil {
		t.Fatalf("no ag-ui/local entry in %+v", file)
	}
	return entry
}

func TestHostedEvalsGrade(t *testing.T) {
	repo := hostedRepo(t)
	f := &fakeConverse{}
	opts := hostedOptions(t, repo, f)
	opts.RetainRoot = t.TempDir()
	rep := &captureReporter{}
	opts.Reporter = rep

	failed, err := Evals(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !failed {
		t.Error("failed = false, want true (one eval fails, one errors)")
	}
	if n := f.calls.Load(); n != 3 {
		t.Errorf("conversations = %d, want 3 (no baseline)", n)
	}
	entry := loadHosted(t, repo)
	h := entry.Header
	if h.Provider != "ag-ui" || h.Model != "local" || h.Display != "Local" || h.Harness != "ag-ui" ||
		h.Pricing != nil || !entry.Executed {
		t.Errorf("header = %+v", h)
	}
	spec, err := evalspec.LoadEvals(filepath.Join(repo.Root, "agents", "support", "evals.json"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := agentContentHash(filepath.Join(repo.Root, "agents", "support"), spec.Evals)
	if h.ContentHash == "" || h.ContentHash != want {
		t.Errorf("content hash = %q, want the agent content hash %q", h.ContentHash, want)
	}
	if entry.Baseline != nil {
		t.Errorf("baseline = %+v, want none for an agent", entry.Baseline)
	}
	byID := byID(entry.Results)

	pass := byID["refund"]
	if pass.Passed == nil || !*pass.Passed {
		for _, e := range pass.Expectations {
			t.Logf("  %s: %v %s", e.Text, boolOf(e.Passed), e.Evidence)
		}
		t.Fatalf("refund = %+v, want pass", pass)
	}
	if pass.Estimate != nil {
		t.Errorf("estimate = %+v, want none (hosted prompts are not counted)", pass.Estimate)
	}
	// 1000 input of which 600 cached → 400 fresh; priced at Claude Sonnet 5.
	if m := pass.Measured; m == nil || *m.InputTokens != 400 || *m.CacheReadTokens != 600 ||
		*m.OutputTokens != 100 || m.CostUSD == nil || *m.CostUSD != 0.0030 {
		t.Errorf("measured = %+v", pass.Measured)
	}
	if c := pass.Conversation; c == nil || c.Runs != 2 || c.Outcome != "completed" || len(c.HITL) != 1 ||
		!c.HITL[0].Answered || len(c.ConformanceViolations) != 1 || c.ConformanceViolations[0].Severity != "warning" {
		t.Errorf("conversation = %+v", pass.Conversation)
	}
	if pass.ExecutionMetrics == nil || pass.ExecutionMetrics.ToolCalls["issue_refund"] != 1 {
		t.Errorf("execution metrics = %+v", pass.ExecutionMetrics)
	}

	stall := byID["stalls"]
	if stall.Passed == nil || *stall.Passed || stall.Conversation.Outcome != "awaiting_input" {
		t.Errorf("stalls = %+v, want a graded failure", stall)
	}
	broken := byID["broken"]
	if broken.RuntimeError != "HTTP 502: bad gateway" || broken.Passed != nil || broken.Conversation == nil {
		t.Errorf("broken = %+v, want a runtime error", broken)
	}
	if s := entry.Summary; *s.Passed != 1 || *s.Failed != 1 || *s.Errored != 1 {
		t.Errorf("summary = %+v", s)
	}

	// The retained run log is the conversation's events.jsonl, and the error
	// line names the endpoint.
	var sawLog, sawError bool
	for _, it := range rep.items {
		if it.Label == "refund" && it.LogPath != "" {
			sawLog = true
			if _, err := os.Stat(filepath.Join(it.WorkspacePath, agui.ArtifactDir, agui.TranscriptMarkdown)); err != nil {
				t.Errorf("transcript.md not retained: %v", err)
			}
		}
		if it.Label == "broken" && strings.Contains(it.Detail, "ag-ui endpoint local runtime failure: HTTP 502") {
			sawError = true
		}
	}
	if !sawLog || !sawError {
		t.Errorf("items = %+v", rep.items)
	}
}

func TestHostedSkipsCountOnlyAndSkillSelections(t *testing.T) {
	repo := hostedRepo(t)
	f := &fakeConverse{}
	opts := hostedOptions(t, repo, f)
	opts.CountOnly = true
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	// A skill selection never drives an agent suite.
	opts = hostedOptions(t, repo, f)
	p := &fakeEvalProvider{}
	opts.Selected = []harness.Selection{{Model: p.canonicalModel(), Harness: p}}
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if n := f.calls.Load(); n != 0 {
		t.Errorf("conversations = %d, want 0", n)
	}
	if path := results.Find(filepath.Join(repo.Root, "agents", "support")); path != "" {
		t.Errorf("results written: %s", path)
	}
}

func TestHostedContextAbort(t *testing.T) {
	repo := hostedRepo(t)
	f := &fakeConverse{err: context.Canceled}
	opts := hostedOptions(t, repo, f)
	if _, err := Evals(context.Background(), opts); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled to abort the sweep", err)
	}
}

func TestHostedUnresolvedEndpoint(t *testing.T) {
	repo := hostedRepo(t)
	opts := hostedOptions(t, repo, &fakeConverse{})
	opts.Agents = nil
	if _, err := Evals(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "not resolved") {
		t.Errorf("err = %v, want an unresolved-endpoint error", err)
	}
}

// TestHostedSelectionFlags: --new leaves a complete hosted result alone (no
// usage or pricing gap), and editing the agent's definition makes --modified
// rerun it — with CaseReasons agreeing with the engine at every step.
func TestHostedSelectionFlags(t *testing.T) {
	repo := hostedRepo(t)
	f := &fakeConverse{}
	opts := hostedOptions(t, repo, f)
	opts.EvalFilter = "refund"
	cat, err := Catalog(opts.Options)
	if err != nil {
		t.Fatal(err)
	}
	reasons := func() plan.CaseReason {
		r := CaseReasons(opts.Options, cat, opts.Selected, plan.Tiers{Evals: true}, "refund")
		return r["ag-ui/local"][plan.CaseRef{Skill: "support", Kind: plan.KindEvals, Case: "refund"}]
	}
	if r := reasons(); !r.New || r.Modified {
		t.Errorf("before any run: reasons = %+v, want new", r)
	}
	if _, err := Evals(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if r := reasons(); r.New || r.Modified || r.Failing {
		t.Errorf("after a passing run: reasons = %+v, want none", r)
	}

	newOpts := opts
	newOpts.New = true
	before := f.calls.Load()
	if _, err := Evals(context.Background(), newOpts); err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != before {
		t.Error("--new reran a complete hosted result")
	}
	need, _ := Needs(newOpts.Options, cat, opts.Selected, plan.Tiers{Evals: true}, "refund")
	if need["ag-ui/local"][plan.CaseRef{Skill: "support", Kind: plan.KindEvals, Case: "refund"}] {
		t.Error("Needs disagrees with the engine under --new")
	}

	// Editing a staged fixture is the eval's change, not the agent's; editing
	// the system prompt is the agent's.
	if err := os.WriteFile(filepath.Join(repo.Root, "agents", "support", "SYSTEM_PROMPT.md"),
		[]byte("Be terse.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := reasons(); !r.Modified {
		t.Errorf("after editing SYSTEM_PROMPT.md: reasons = %+v, want modified", r)
	}
	modOpts := opts
	modOpts.Modified = true
	before = f.calls.Load()
	if _, err := Evals(context.Background(), modOpts); err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != before+1 {
		t.Errorf("--modified ran %d conversations, want 1", f.calls.Load()-before)
	}
	if r := reasons(); r.Modified {
		t.Errorf("after the rerun: reasons = %+v, want current", r)
	}
	// Editing agent.py does too.
	if err := os.WriteFile(filepath.Join(repo.Root, "agents", "support", "agent.py"),
		[]byte("def run(): return 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := reasons(); !r.Modified {
		t.Errorf("after editing agent.py: reasons = %+v, want modified", r)
	}
}

// TestHostedEndToEnd drives a real AG-UI conversation (agui.Converse against
// an httptest agent): a frontend tool round trip, an interrupt resumed with a
// scripted payload, and a follow-up turn, graded into results.
func TestHostedEndToEnd(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	evals := `{"agent": {"name": "concierge", "endpoints": {"local": {"url": "http://placeholder/"}}},
		"evals": [{"id": "book", "prompt": "Book a table for two.",
			"tools": [{"name": "pick_time", "description": "Ask the user for a time"}],
			"responses": [
				{"tool": "pick_time", "result": "19:30"},
				{"interrupt": "confirmation", "message": "(?i)deposit", "payload": {"approved": true}}
			],
			"followups": ["Thanks!"],
			"assertions": [
				{"type": "outcome", "expect": "completed"},
				{"type": "hitl", "tool": "pick_time"},
				{"type": "hitl", "reason": "confirmation"},
				{"type": "tool_call", "tool": "pick_time", "result": "19:30"},
				{"type": "state", "pointer": "/booking/time", "equals": "19:30"},
				{"type": "conformance"},
				{"type": "regex", "pattern": "You're welcome"}
			]}]}`
	if err := os.WriteFile(filepath.Join(dir, "evals.json"), []byte(evals), 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var inputs []agui.RunAgentInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in agui.RunAgentInput
		_ = json.NewDecoder(r.Body).Decode(&in)
		mu.Lock()
		inputs = append(inputs, in)
		n := len(inputs)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		start := fmt.Sprintf(`{"type":"RUN_STARTED","threadId":%q,"runId":%q}`, in.ThreadID, in.RunID)
		finish := func(extra string) string {
			return fmt.Sprintf(`{"type":"RUN_FINISHED","threadId":%q,"runId":%q%s}`, in.ThreadID, in.RunID, extra)
		}
		var events []string
		switch n {
		case 1:
			events = []string{start,
				`{"type":"TOOL_CALL_START","toolCallId":"t1","toolCallName":"pick_time"}`,
				`{"type":"TOOL_CALL_END","toolCallId":"t1"}`, finish("")}
		case 2:
			events = []string{start,
				`{"type":"STATE_SNAPSHOT","snapshot":{"booking":{"time":"19:30"}}}`,
				finish(`,"outcome":{"type":"interrupt","interrupts":[{"id":"i1","reason":"confirmation","message":"Pay a deposit?"}]}`)}
		case 3:
			events = []string{start, `{"type":"TEXT_MESSAGE_CHUNK","messageId":"m1","delta":"Booked for 19:30."}`, finish("")}
		default:
			events = []string{start, `{"type":"TEXT_MESSAGE_CHUNK","messageId":"m2","delta":"You're welcome!"}`, finish("")}
		}
		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
	defer srv.Close()

	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	opts := hostedOptions(t, repo, nil)
	opts.Converse = nil // the real client
	opts.Agents = map[string]map[string]agui.Endpoint{"concierge": {"local": {URL: srv.URL}}}
	failed, err := Evals(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	file, _, _ := results.LoadDir(dir, layout.AgentsGroup, "concierge")
	entry := file.Eval("ag-ui/local")
	if entry == nil || len(entry.Results) != 1 {
		t.Fatalf("entry = %+v", entry)
	}
	r := entry.Results[0]
	if failed || r.Passed == nil || !*r.Passed {
		for _, e := range r.Expectations {
			t.Logf("  %s: %v %s", e.Text, boolOf(e.Passed), e.Evidence)
		}
		t.Fatalf("book = %+v (runtime error %q)", r, r.RuntimeError)
	}
	if len(inputs) != 4 || len(inputs[2].Resume) != 1 || string(inputs[2].Resume[0].Payload) != `{"approved":true}` {
		t.Errorf("inputs = %+v", inputs)
	}
	if r.Conversation == nil || r.Conversation.Runs != 4 || len(r.Conversation.HITL) != 2 {
		t.Errorf("conversation = %+v", r.Conversation)
	}
}

func boolOf(p *bool) string {
	if p == nil {
		return "skip"
	}
	return fmt.Sprint(*p)
}
