// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// turn renders one run's response body from the request it answers.
type turn func(in RunAgentInput) string

// fakeAgent is a scripted AG-UI agent: request i is answered by turns[i].
type fakeAgent struct {
	t       *testing.T
	turns   []turn
	mu      sync.Mutex
	inputs  []RunAgentInput
	headers []http.Header
}

func (f *fakeAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var in RunAgentInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		f.t.Errorf("decoding input: %v", err)
		http.Error(w, "bad input", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.inputs = append(f.inputs, in)
	f.headers = append(f.headers, r.Header.Clone())
	i := len(f.inputs) - 1
	f.mu.Unlock()
	if i >= len(f.turns) {
		f.t.Errorf("unexpected run %d", i+1)
		http.Error(w, "no more turns", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, f.turns[i](in))
}

// serve starts a fake agent and returns its endpoint.
func serve(t *testing.T, turns ...turn) (*fakeAgent, Endpoint) {
	t.Helper()
	f := &fakeAgent{t: t, turns: turns}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, Endpoint{URL: srv.URL}
}

// sse frames events as an SSE body.
func sse(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString("data: " + e + "\n\n")
	}
	return b.String()
}

// runBody wraps events in RUN_STARTED/RUN_FINISHED for in; finish is the
// RUN_FINISHED's extra fields (e.g. `,"outcome":{...}`).
func runBody(in RunAgentInput, finish string, events ...string) string {
	all := []string{fmt.Sprintf(`{"type":"RUN_STARTED","threadId":%q,"runId":%q}`, in.ThreadID, in.RunID)}
	all = append(all, events...)
	all = append(all, fmt.Sprintf(`{"type":"RUN_FINISHED","threadId":%q,"runId":%q%s}`, in.ThreadID, in.RunID, finish))
	return sse(all...)
}

func say(id, text string) []string {
	return []string{
		fmt.Sprintf(`{"type":"TEXT_MESSAGE_START","messageId":%q,"role":"assistant"}`, id),
		fmt.Sprintf(`{"type":"TEXT_MESSAGE_CONTENT","messageId":%q,"delta":%q}`, id, text),
		fmt.Sprintf(`{"type":"TEXT_MESSAGE_END","messageId":%q}`, id),
	}
}

func call(id, name, args string) []string {
	return []string{
		fmt.Sprintf(`{"type":"TOOL_CALL_START","toolCallId":%q,"toolCallName":%q}`, id, name),
		fmt.Sprintf(`{"type":"TOOL_CALL_ARGS","toolCallId":%q,"delta":%q}`, id, args),
		fmt.Sprintf(`{"type":"TOOL_CALL_END","toolCallId":%q}`, id),
	}
}

func cat(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

func converse(t *testing.T, ep Endpoint, s Script) *Transcript {
	t.Helper()
	tr, err := Converse(context.Background(), ep, s)
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	return tr
}

func errorViolations(tr *Transcript) []Violation {
	var out []Violation
	for _, v := range tr.Violations {
		if v.Severity == SeverityError {
			out = append(out, v)
		}
	}
	return out
}

func TestConverseFrontendToolRoundTrip(t *testing.T) {
	agent, ep := serve(t,
		func(in RunAgentInput) string {
			return runBody(in, "", cat(say("m1", "Let me confirm."),
				call("c1", "confirm_refund", `{"orderId":"1234"}`))...)
		},
		func(in RunAgentInput) string {
			return runBody(in, "", cat(say("m2", "Refunded."), call("c2", "issue_refund", `{}`),
				[]string{`{"type":"TOOL_CALL_RESULT","messageId":"m3","toolCallId":"c2","content":"{\"status\":\"ok\"}"}`})...)
		},
	)
	ep.ForwardedProps = json.RawMessage(`{"tenant":"eval","channel":"api"}`)
	tr := converse(t, ep, Script{
		Prompt:         "Refund order 1234.",
		Tools:          []Tool{{Name: "confirm_refund", Description: "Ask the user"}},
		ForwardedProps: json.RawMessage(`{"channel":"web"}`),
		Responses: []Response{{Tool: "^confirm_refund$", Args: `"orderId":\s*"1234"`,
			Result: json.RawMessage(`{"approved": true}`)}},
	})
	if tr.Outcome != Completed || tr.RuntimeError != "" {
		t.Fatalf("outcome = %s (%s), want completed", tr.Outcome, tr.RuntimeError)
	}
	if len(tr.Runs) != 2 || tr.Output != "Let me confirm.\n\nRefunded." {
		t.Errorf("runs = %d, output = %q", len(tr.Runs), tr.Output)
	}
	first := agent.inputs[0]
	if first.ProtocolVersion != "1.0" || string(first.State) != "{}" || len(first.Tools) != 1 ||
		first.Context == nil || string(first.ForwardedProps) != `{"channel":"web","tenant":"eval"}` {
		t.Errorf("first input = %+v", first)
	}
	if h := agent.headers[0]; h.Get("Accept") != "text/event-stream" || h.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", h)
	}
	second := agent.inputs[1]
	if second.ThreadID != first.ThreadID || second.RunID == first.RunID {
		t.Errorf("thread/run ids: %s/%s then %s/%s", first.ThreadID, first.RunID, second.ThreadID, second.RunID)
	}
	msgs := second.Messages
	if len(msgs) != 3 || msgs[0].Role != "user" || msgs[1].Role != "assistant" || msgs[2].Role != "tool" {
		t.Fatalf("second run messages = %+v", msgs)
	}
	if got := msgs[1].ToolCalls; len(got) != 1 || got[0].Function.Arguments != `{"orderId":"1234"}` ||
		msgs[1].Text() != "Let me confirm." {
		t.Errorf("assistant message = %+v", msgs[1])
	}
	if msgs[2].ToolCallID != "c1" || msgs[2].Text() != `{"approved":true}` {
		t.Errorf("tool answer = %+v", msgs[2])
	}
	if len(tr.HITL) != 1 || !tr.HITL[0].Answered || tr.HITL[0].Kind != HITLTool || tr.HITL[0].Name != "confirm_refund" {
		t.Errorf("hitl = %+v", tr.HITL)
	}
	if len(tr.ToolCalls) != 2 || !tr.ToolCalls[0].Frontend || *tr.ToolCalls[0].Result != `{"approved":true}` ||
		*tr.ToolCalls[1].Result != `{"status":"ok"}` || tr.ToolCalls[1].Run != 2 {
		t.Errorf("tool calls = %+v", tr.ToolCalls)
	}
	if v := errorViolations(tr); len(v) != 0 {
		t.Errorf("violations = %v", v)
	}
}

func TestConversePendingToolCallIDs(t *testing.T) {
	agent, ep := serve(t,
		func(in RunAgentInput) string {
			// "ask" was not declared, but the agent lists it as pending; the
			// declared "lookup" was answered in-stream and is not pending.
			return runBody(in, `,"outcome":{"type":"success","pendingToolCallIds":["c1"]}`,
				cat(call("c1", "ask", `{}`), call("c2", "lookup", `{}`),
					[]string{`{"type":"TOOL_CALL_RESULT","messageId":"r2","toolCallId":"c2","content":"found"}`})...)
		},
		func(in RunAgentInput) string { return runBody(in, "", say("m", "done")...) },
	)
	tr := converse(t, ep, Script{
		Prompt:    "go",
		Tools:     []Tool{{Name: "lookup", Description: "d"}},
		Responses: []Response{{Tool: "ask", Error: "user declined"}},
	})
	if tr.Outcome != Completed {
		t.Fatalf("outcome = %s (%s)", tr.Outcome, tr.RuntimeError)
	}
	last := agent.inputs[1].Messages[len(agent.inputs[1].Messages)-1]
	if last.ToolCallID != "c1" || last.Error != "user declined" {
		t.Errorf("tool answer = %+v", last)
	}
	if len(tr.HITL) != 1 || tr.HITL[0].ID != "c1" {
		t.Errorf("hitl = %+v", tr.HITL)
	}
}

func TestConverseInterruptResume(t *testing.T) {
	agent, ep := serve(t,
		func(in RunAgentInput) string {
			return runBody(in, `,"outcome":{"type":"interrupt","interrupts":[`+
				`{"id":"i1","reason":"confirmation","message":"Refund 1234?"},`+
				`{"id":"i2","reason":"tool_call","toolCallId":"c1"}]}`,
				call("c1", "send_email", `{"to":"a@b"}`)...)
		},
		func(in RunAgentInput) string { return runBody(in, "", say("m", "ok")...) },
	)
	tr := converse(t, ep, Script{
		Prompt: "Refund it",
		Responses: []Response{
			{Interrupt: "^tool_call$", Tool: "^send_email$", Status: ResumeCancelled},
			{Interrupt: "confirmation", Message: "(?i)refund", Payload: json.RawMessage(`{"approved":true}`)},
		},
	})
	if tr.Outcome != Completed {
		t.Fatalf("outcome = %s (%s)", tr.Outcome, tr.RuntimeError)
	}
	resume := agent.inputs[1].Resume
	if len(resume) != 2 || resume[0].InterruptID != "i1" || resume[0].Status != ResumeResolved ||
		string(resume[0].Payload) != `{"approved":true}` || resume[1].InterruptID != "i2" ||
		resume[1].Status != ResumeCancelled || resume[1].Payload != nil {
		t.Errorf("resume = %+v", resume)
	}
	if agent.inputs[1].ThreadID != agent.inputs[0].ThreadID {
		t.Error("resume changed the thread id")
	}
	if len(tr.HITL) != 2 || !tr.HITL[0].Answered || tr.HITL[1].Name != "send_email" || tr.HITL[1].Reason != "tool_call" {
		t.Errorf("hitl = %+v", tr.HITL)
	}
}

func TestConverseUncoveredInterruptAwaitsInput(t *testing.T) {
	agent, ep := serve(t, func(in RunAgentInput) string {
		return runBody(in, `,"outcome":{"type":"interrupt","interrupts":[`+
			`{"id":"i1","reason":"confirmation"},{"id":"i2","reason":"input_required"}]}`)
	})
	tr := converse(t, ep, Script{Prompt: "p", Responses: []Response{{Interrupt: "confirmation"}}})
	if tr.Outcome != AwaitingInput || len(agent.inputs) != 1 {
		t.Errorf("outcome = %s after %d runs, want awaiting_input after 1", tr.Outcome, len(agent.inputs))
	}
	if len(tr.HITL) != 2 || tr.HITL[0].Answered || tr.HITL[1].Answered {
		t.Errorf("a partial resume must not be sent: hitl = %+v", tr.HITL)
	}
}

func TestConverseUnansweredToolAwaitsInput(t *testing.T) {
	_, ep := serve(t, func(in RunAgentInput) string {
		return runBody(in, "", call("c1", "pick_date", `{}`)...)
	})
	tr := converse(t, ep, Script{
		Prompt: "p", Tools: []Tool{{Name: "pick_date", Description: "d"}},
		Responses: []Response{{Tool: "^other$", Result: json.RawMessage(`"x"`)}},
	})
	if tr.Outcome != AwaitingInput || len(tr.HITL) != 1 || tr.HITL[0].Answered {
		t.Errorf("outcome = %s, hitl = %+v", tr.Outcome, tr.HITL)
	}
}

func TestConverseCancelled(t *testing.T) {
	_, ep := serve(t, func(in RunAgentInput) string {
		return runBody(in, `,"outcome":{"type":"cancelled"}`, say("m", "stopping")...)
	})
	tr := converse(t, ep, Script{Prompt: "p", FollowUps: []string{"never sent"}})
	if tr.Outcome != Cancelled || len(tr.Runs) != 1 {
		t.Errorf("outcome = %s after %d runs", tr.Outcome, len(tr.Runs))
	}
}

func TestConverseFollowUps(t *testing.T) {
	agent, ep := serve(t,
		func(in RunAgentInput) string { return runBody(in, "", say("m1", "first")...) },
		func(in RunAgentInput) string { return runBody(in, "", say("m2", "second")...) },
	)
	tr := converse(t, ep, Script{Prompt: "p", FollowUps: []string{"and the receipt?"}})
	if tr.Outcome != Completed || tr.Output != "first\n\nsecond" {
		t.Fatalf("outcome = %s, output = %q", tr.Outcome, tr.Output)
	}
	msgs := agent.inputs[1].Messages
	if last := msgs[len(msgs)-1]; last.Role != "user" || last.Text() != "and the receipt?" {
		t.Errorf("follow-up = %+v", last)
	}
	if len(tr.Runs[1].Sent) != 1 || tr.Runs[1].Sent[0].Text() != "and the receipt?" {
		t.Errorf("run 2 sent = %+v", tr.Runs[1].Sent)
	}
}

func TestConverseMaxTurns(t *testing.T) {
	loop := func(in RunAgentInput) string {
		return runBody(in, "", call("c"+in.RunID, "again", `{}`)...)
	}
	agent, ep := serve(t, loop, loop, loop)
	tr := converse(t, ep, Script{
		Prompt: "p", Tools: []Tool{{Name: "again", Description: "d"}}, MaxRuns: 3,
		Responses: []Response{{Tool: "again", Result: json.RawMessage(`"more"`), Repeat: true}},
	})
	if tr.Outcome != MaxTurns || len(agent.inputs) != 3 {
		t.Errorf("outcome = %s after %d runs, want max_turns after 3", tr.Outcome, len(agent.inputs))
	}
}

func TestConverseRuntimeErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		timeout time.Duration
		want    string
	}{
		{"non-2xx", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "bad token", http.StatusUnauthorized)
		}, 0, "HTTP 401: bad token"},
		{"not SSE", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"error":"use POST /agent"}`)
		}, 0, `not an SSE stream (Content-Type "application/json")`},
		{"RUN_ERROR", func(w http.ResponseWriter, r *http.Request) {
			in := decodeInput(r)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, sse(fmt.Sprintf(`{"type":"RUN_STARTED","threadId":%q,"runId":%q}`, in.ThreadID, in.RunID),
				`{"type":"RUN_ERROR","message":"model overloaded","code":"overloaded"}`))
		}, 0, "RUN_ERROR: model overloaded (overloaded)"},
		{"truncated", func(w http.ResponseWriter, r *http.Request) {
			in := decodeInput(r)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, sse(fmt.Sprintf(`{"type":"RUN_STARTED","threadId":%q,"runId":%q}`, in.ThreadID, in.RunID)))
		}, 0, "truncated"},
		{"empty stream", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
		}, 0, "no events"},
		{"bad first event", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, sse(`{"type":"TEXT_MESSAGE_START","messageId":"m"}`))
		}, 0, "did not begin with RUN_STARTED"},
		{"malformed event", func(w http.ResponseWriter, r *http.Request) {
			in := decodeInput(r)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, sse(fmt.Sprintf(`{"type":"RUN_STARTED","threadId":%q,"runId":%q}`, in.ThreadID, in.RunID),
				`{"type":"TEXT_MESSAGE_CONTENT","messageId":"m","delta":5}`))
		}, 0, "invalid event 2"},
		{"timeout", func(w http.ResponseWriter, r *http.Request) {
			// Drain the body so the server notices the client hanging up.
			_, _ = io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
		}, 50 * time.Millisecond, "timed out after 50ms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			tr := converse(t, Endpoint{URL: srv.URL}, Script{Prompt: "p", Timeout: tt.timeout})
			if tr.Outcome != Errored || !strings.Contains(tr.RuntimeError, tt.want) {
				t.Errorf("outcome = %s, runtime error = %q, want %q", tr.Outcome, tr.RuntimeError, tt.want)
			}
			if len(tr.Runs) != 1 {
				t.Errorf("runs = %d, want 1", len(tr.Runs))
			}
		})
	}
}

func decodeInput(r *http.Request) RunAgentInput {
	var in RunAgentInput
	_ = json.NewDecoder(r.Body).Decode(&in)
	return in
}

func TestConverseTransportErrorHidesURL(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/?token=s3cret"
	srv.Close()
	tr := converse(t, Endpoint{URL: url}, Script{Prompt: "p"})
	if tr.Outcome != Errored || strings.Contains(tr.RuntimeError, "s3cret") {
		t.Errorf("runtime error = %q, want a transport error without the URL", tr.RuntimeError)
	}
}

func TestConverseContextCancelAborts(t *testing.T) {
	entered := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-entered
		cancel()
	}()
	_, err := Converse(ctx, Endpoint{URL: srv.URL}, Script{Prompt: "p", Timeout: time.Minute})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestConverseReplayedRunsAreNotReduced(t *testing.T) {
	_, ep := serve(t, func(in RunAgentInput) string {
		old := []string{fmt.Sprintf(`{"type":"RUN_STARTED","threadId":%q,"runId":"earlier"}`, in.ThreadID)}
		old = append(old, say("m0", "replayed")...)
		old = append(old, fmt.Sprintf(`{"type":"RUN_FINISHED","threadId":%q,"runId":"earlier"}`, in.ThreadID))
		return sse(old...) + runBody(in, "", say("m1", "fresh")...)
	})
	tr := converse(t, ep, Script{Prompt: "p"})
	if tr.Outcome != Completed || tr.Output != "fresh" {
		t.Errorf("outcome = %s, output = %q", tr.Outcome, tr.Output)
	}
	if len(tr.Violations) != 0 {
		t.Errorf("violations = %v", tr.Violations)
	}
	if tr.Runs[0].Events != 10 {
		t.Errorf("events = %d, want 10 (replayed run included)", tr.Runs[0].Events)
	}
}

func TestConverseStateAndSnapshots(t *testing.T) {
	agent, ep := serve(t,
		func(in RunAgentInput) string {
			return runBody(in, "",
				`{"type":"STATE_DELTA","delta":[{"op":"add","path":"/refunds","value":[]}]}`,
				`{"type":"STATE_DELTA","delta":[{"op":"add","path":"/refunds/-","value":{"orderId":"1234"}}]}`,
				fmt.Sprintf(`{"type":"MESSAGES_SNAPSHOT","messages":[%s,`+
					`{"id":"a1","role":"assistant","content":"from snapshot","toolCalls":`+
					`[{"id":"c9","type":"function","function":{"name":"audit","arguments":"{}"}}]},`+
					`{"id":"t9","role":"tool","toolCallId":"c9","content":"logged"},`+
					`{"id":"x","role":"activity","activityType":"plan","content":{}}]}`,
					mustJSON(in.Messages[0])),
			)
		},
		func(in RunAgentInput) string { return runBody(in, "", say("m", "bye")...) },
	)
	tr := converse(t, ep, Script{
		Prompt: "p", State: json.RawMessage(`{"customer":"c-42"}`), FollowUps: []string{"more"},
		Context: []Context{{Description: "locale", Value: "en-GB"}},
	})
	if tr.Outcome != Completed {
		t.Fatalf("outcome = %s (%s)", tr.Outcome, tr.RuntimeError)
	}
	if got := string(agent.inputs[0].State); got != `{"customer":"c-42"}` {
		t.Errorf("initial state sent = %s", got)
	}
	var state map[string]any
	_ = json.Unmarshal(agent.inputs[1].State, &state)
	if refunds, _ := state["refunds"].([]any); len(refunds) != 1 || state["customer"] != "c-42" {
		t.Errorf("state carried into run 2 = %v", state)
	}
	if string(tr.State) != string(agent.inputs[1].State) {
		t.Errorf("final state = %s", tr.State)
	}
	msgs := agent.inputs[1].Messages
	for _, m := range msgs {
		if m.Role == "activity" {
			t.Error("activity messages must be stripped before sending")
		}
	}
	if len(msgs) != 4 || msgs[1].ID != "a1" || msgs[3].Text() != "more" {
		t.Errorf("run 2 messages = %+v", msgs)
	}
	if tr.Output != "from snapshot\n\nbye" {
		t.Errorf("output = %q", tr.Output)
	}
	if len(tr.ToolCalls) != 1 || tr.ToolCalls[0].Name != "audit" || *tr.ToolCalls[0].Result != "logged" {
		t.Errorf("tool calls = %+v", tr.ToolCalls)
	}
	if len(agent.inputs[0].Context) != 1 {
		t.Errorf("context not sent: %+v", agent.inputs[0].Context)
	}
}

func TestConverseChunksAndUsage(t *testing.T) {
	_, ep := serve(t,
		func(in RunAgentInput) string {
			return runBody(in, `,"usage":[{"provider":"anthropic","model":"claude-sonnet-5","inputTokens":100,`+
				`"cachedInputTokens":40,"outputTokens":10}]`,
				`{"type":"TEXT_MESSAGE_CHUNK","messageId":"m1","delta":"Hel"}`,
				`{"type":"TEXT_MESSAGE_CHUNK","delta":"lo"}`,
				`{"type":"TOOL_CALL_CHUNK","toolCallId":"c1","toolCallName":"t","delta":"{\"a\""}`,
				`{"type":"TOOL_CALL_CHUNK","toolCallId":"c1","delta":":1}"}`,
			)
		},
		func(in RunAgentInput) string {
			return runBody(in, `,"usage":[{"provider":"anthropic","model":"claude-sonnet-5","inputTokens":50,`+
				`"outputTokens":5},{"provider":"openai","model":"gpt","outputTokens":1}]`, say("m2", "again")...)
		},
	)
	tr := converse(t, ep, Script{Prompt: "p", FollowUps: []string{"f"}})
	if tr.Output != "Hello\n\nagain" {
		t.Errorf("output = %q", tr.Output)
	}
	if len(tr.ToolCalls) != 1 || tr.ToolCalls[0].Args != `{"a":1}` {
		t.Errorf("tool calls = %+v", tr.ToolCalls)
	}
	if len(tr.Usage) != 2 || *tr.Usage[0].InputTokens != 150 || *tr.Usage[0].CachedInputTokens != 40 ||
		*tr.Usage[0].OutputTokens != 15 || tr.Usage[1].InputTokens != nil || *tr.Usage[1].OutputTokens != 1 {
		t.Errorf("usage = %s", mustJSON(tr.Usage))
	}
	if v := errorViolations(tr); len(v) != 0 {
		t.Errorf("violations = %v", v)
	}
}

func TestConverseInvalidResponsePattern(t *testing.T) {
	tr := converse(t, Endpoint{URL: "http://127.0.0.1:1"}, Script{Prompt: "p", Responses: []Response{{Tool: "("}}})
	if tr.Outcome != Errored || !strings.Contains(tr.RuntimeError, "responses[0].tool") || len(tr.Runs) != 0 {
		t.Errorf("outcome = %s, error = %q, runs = %d", tr.Outcome, tr.RuntimeError, len(tr.Runs))
	}
}

func TestWriteArtifacts(t *testing.T) {
	agent, ep := serve(t, func(in RunAgentInput) string {
		return runBody(in, "", append(say("m1", "hi"), `{"type":"STATE_SNAPSHOT","snapshot":{"k":"v"}}`)...)
	})
	ep.Headers = map[string]string{"Authorization": "Bearer s3cret"}
	tr := converse(t, ep, Script{Prompt: "hello"})
	if agent.headers[0].Get("Authorization") != "Bearer s3cret" {
		t.Errorf("header not sent: %v", agent.headers[0])
	}
	ws := t.TempDir()
	if err := WriteArtifacts(ws, tr); err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(ws, ArtifactDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	events := strings.Split(strings.TrimSpace(read(EventsFile)), "\n")
	if len(events) != 6 || !strings.HasPrefix(events[0], `{"run":1,"event":{"type":"RUN_STARTED"`) {
		t.Errorf("events.jsonl = %q", events)
	}
	var back Transcript
	if err := json.Unmarshal([]byte(read(TranscriptFile)), &back); err != nil || back.Outcome != Completed {
		t.Errorf("transcript.json: %v %+v", err, back)
	}
	if !strings.Contains(read(TranscriptMarkdown), "> hi") {
		t.Errorf("transcript.md = %s", read(TranscriptMarkdown))
	}
	if strings.TrimSpace(read(StateFile)) != "{\n  \"k\": \"v\"\n}" {
		t.Errorf("state.json = %q", read(StateFile))
	}
	for _, name := range []string{EventsFile, TranscriptFile, TranscriptMarkdown, StateFile} {
		if strings.Contains(read(name), "s3cret") {
			t.Errorf("%s leaks the header secret", name)
		}
	}
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// TestConverseMaxTurnsRecordsNoUnsentAnswers: at the run cap, a matched
// answer that no run will carry is not recorded as given.
func TestConverseMaxTurnsRecordsNoUnsentAnswers(t *testing.T) {
	_, ep := serve(t, func(in RunAgentInput) string {
		return runBody(in, "", call("c1", "confirm", `{}`)...)
	})
	tr := converse(t, ep, Script{
		Prompt: "p", Tools: []Tool{{Name: "confirm", Description: "d"}}, MaxRuns: 1,
		Responses: []Response{{Tool: "confirm", Result: json.RawMessage(`"yes"`)}},
	})
	if tr.Outcome != MaxTurns {
		t.Fatalf("outcome = %s", tr.Outcome)
	}
	if len(tr.HITL) != 1 || tr.HITL[0].Answered || tr.ToolCalls[0].Result != nil {
		t.Errorf("hitl = %+v, calls = %+v; the unsent answer must not be recorded", tr.HITL, tr.ToolCalls)
	}
	if last := tr.Messages[len(tr.Messages)-1]; last.Role == "tool" {
		t.Errorf("history gained an unsent tool message: %+v", last)
	}
}

// TestConverseToleratesBrokenTailAfterFinish: once the requested run has
// finished, a connection that breaks or stays open loses nothing.
func TestConverseToleratesBrokenTailAfterFinish(t *testing.T) {
	defer func(d time.Duration) { finishGrace = d }(finishGrace)
	finishGrace = 50 * time.Millisecond
	for _, tc := range []struct {
		name string
		tail func(w http.ResponseWriter, r *http.Request)
	}{
		{"connection reset", func(w http.ResponseWriter, _ *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close() // no terminating chunk: the client sees an unexpected EOF
			}
		}},
		{"held open", func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				in := decodeInput(r)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, runBody(in, "", say("m1", "hello")...))
				w.(http.Flusher).Flush()
				tc.tail(w, r)
			}))
			defer srv.Close()
			tr := converse(t, Endpoint{URL: srv.URL}, Script{Prompt: "p", Timeout: 10 * time.Second})
			if tr.Outcome != Completed || tr.Output != "hello" || tr.RuntimeError != "" {
				t.Errorf("outcome = %s, output = %q, error = %q", tr.Outcome, tr.Output, tr.RuntimeError)
			}
			var warned bool
			for _, v := range tr.Violations {
				warned = warned || (v.Rule == RuleTransport && v.Severity == SeverityWarning)
			}
			if !warned {
				t.Errorf("violations = %v, want a transport warning", tr.Violations)
			}
		})
	}
}

// TestConverseRefusesCrossOriginRedirects: endpoint headers carry
// credentials, so the default client never follows a redirect off-origin.
func TestConverseRefusesCrossOriginRedirects(t *testing.T) {
	var leaked atomicBool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "" {
			leaked.set()
		}
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	defer other.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	tr := converse(t, Endpoint{URL: redirect.URL, Headers: map[string]string{"X-Api-Key": "s3cret"}},
		Script{Prompt: "p"})
	if tr.Outcome != Errored || !strings.Contains(tr.RuntimeError, "another origin") || leaked.get() {
		t.Errorf("outcome = %s, error = %q, leaked = %v", tr.Outcome, tr.RuntimeError, leaked.get())
	}
	// A redirect within the endpoint's origin is followed.
	same := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusPermanentRedirect)
			return
		}
		in := decodeInput(r)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, runBody(in, "", say("m", "moved")...))
	}))
	defer same.Close()
	if tr := converse(t, Endpoint{URL: same.URL + "/old"}, Script{Prompt: "p"}); tr.Output != "moved" {
		t.Errorf("same-origin redirect: outcome = %s, error = %q", tr.Outcome, tr.RuntimeError)
	}
}

// TestConverseScrubsSecrets: an endpoint secret never reaches a recorded
// error, even when the server echoes it or it names the host.
func TestConverseScrubsSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad token "+r.Header.Get("Authorization"), http.StatusUnauthorized)
	}))
	defer srv.Close()
	tr := converse(t, Endpoint{URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer s3cret"},
		Secrets: []string{"s3cret"}}, Script{Prompt: "p"})
	if strings.Contains(tr.RuntimeError, "s3cret") || strings.Contains(tr.Runs[0].Error, "s3cret") ||
		!strings.Contains(tr.RuntimeError, "[redacted]") {
		t.Errorf("runtime error = %q, run error = %q", tr.RuntimeError, tr.Runs[0].Error)
	}
}

// atomicBool is a race-safe flag for handler-side observations.
type atomicBool struct {
	mu sync.Mutex
	v  bool
}

func (b *atomicBool) set() { b.mu.Lock(); b.v = true; b.mu.Unlock() }
func (b *atomicBool) get() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.v
}
