// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package agui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultMaxRuns caps a conversation's runs when the script sets no limit.
const DefaultMaxRuns = 20

// Conversation outcomes: how a scripted conversation ended.
const (
	// Completed: the last run finished with nothing pending and every
	// scripted follow-up was sent.
	Completed = "completed"
	// AwaitingInput: the agent asked for input (a frontend tool call or an
	// interrupt) that no scripted response matched.
	AwaitingInput = "awaiting_input"
	// Cancelled: a run finished with the cancelled outcome.
	Cancelled = "cancelled"
	// MaxTurns: the run cap was reached with something still to send.
	MaxTurns = "max_turns"
	// Errored: a runtime error ended the conversation (see
	// Transcript.RuntimeError); it is not a gradable outcome.
	Errored = "error"
)

// Outcomes is the closed set of gradable conversation outcomes.
var Outcomes = []string{Completed, AwaitingInput, Cancelled, MaxTurns}

// Endpoint is one resolved agent endpoint: its URL and request headers with
// any ${VAR} references already expanded, and the forwardedProps every run
// carries. Headers are sent, never recorded.
type Endpoint struct {
	URL            string
	Headers        map[string]string
	ForwardedProps json.RawMessage
	// Secrets are values (expanded ${VAR}s) scrubbed from every error the
	// transcript records, so a failure message can never carry a credential
	// or a secret-bearing host into results.
	Secrets []string
	// Client sends the requests; nil uses a default client that follows
	// redirects only within the endpoint's origin. The conversation timeout
	// comes from the context, so the client needs none of its own.
	Client *http.Client
}

// Script is one eval's side of the conversation: the opening prompt, what
// every run carries (frontend tools, context, initial state, forwarded
// props), the canned answers to the agent's human-in-the-loop requests, and
// the follow-up user turns sent once nothing is pending.
type Script struct {
	Prompt         string
	Tools          []Tool
	State          json.RawMessage // initial shared state; nil = {}
	Context        []Context
	ForwardedProps json.RawMessage // merged over the endpoint's (top-level keys)
	Responses      []Response
	FollowUps      []string
	MaxRuns        int           // run cap; <= 0 = DefaultMaxRuns
	Timeout        time.Duration // bounds the whole conversation; 0 = none
}

// Response is one canned answer to a human-in-the-loop request. It answers a
// pending frontend tool call when Interrupt is empty — Tool (a regex on the
// tool name) and optional Args (a regex on its JSON arguments) select the
// call, and Result (a JSON string is sent verbatim, any other value
// compactly serialized) or Error is the answer. It answers an interrupt
// when Interrupt (a regex on the interrupt's reason) is set — optional
// Message matches its message and Tool the name of its linked tool call,
// and Status (resolved by default, or cancelled) and Payload form the resume
// entry. Responses match in any order and each is used once unless Repeat.
type Response struct {
	Tool      string
	Args      string
	Result    json.RawMessage
	Error     string
	Interrupt string
	Message   string
	Status    string
	Payload   json.RawMessage
	Repeat    bool
}

// Transcript is the record of one conversation. Events holds every event
// read, tagged with its run, for the events.jsonl artifact.
type Transcript struct {
	Protocol     string          `json:"protocol"`
	ThreadID     string          `json:"thread_id"`
	Outcome      string          `json:"outcome"`
	RuntimeError string          `json:"runtime_error,omitempty"`
	Runs         []Run           `json:"runs"`
	Messages     []Message       `json:"messages"`
	ToolCalls    []ToolCall      `json:"tool_calls,omitempty"`
	HITL         []HITLRequest   `json:"hitl,omitempty"`
	State        json.RawMessage `json:"state"`
	Usage        []TokenUsage    `json:"usage,omitempty"`
	Violations   []Violation     `json:"violations,omitempty"`
	// Output is the assistant text of every run, joined by blank lines: the
	// text regex assertions and the judge grade.
	Output  string          `json:"output"`
	Seconds float64         `json:"seconds"`
	Events  []RecordedEvent `json:"-"`
}

// Run is one request/response exchange of the conversation.
type Run struct {
	Index  int           `json:"index"` // 1-based
	RunID  string        `json:"run_id"`
	Sent   []Message     `json:"sent,omitempty"`   // messages this client added for the run
	Resume []ResumeEntry `json:"resume,omitempty"` // interrupt answers the run carried
	Status int           `json:"http_status,omitempty"`
	Items  []Item        `json:"items,omitempty"` // what the agent produced, in stream order
	// Outcome is how the run ended: success, interrupt, or cancelled (the
	// RUN_FINISHED outcome); error (RUN_ERROR); truncated (no terminal
	// event); or rejected (transport, HTTP, or stream-level failure).
	Outcome          string          `json:"outcome"`
	Interrupts       []Interrupt     `json:"interrupts,omitempty"`
	PendingToolCalls []string        `json:"pending_tool_calls,omitempty"`
	Result           json.RawMessage `json:"result,omitempty"`
	Error            string          `json:"error,omitempty"`
	Usage            []TokenUsage    `json:"usage,omitempty"`
	Events           int             `json:"events"`
	Seconds          float64         `json:"seconds"`
}

// Item is one thing a run produced, resolved to its final content.
type Item struct {
	Kind       string `json:"kind"` // text, reasoning, tool_call, tool_result
	Role       string `json:"role,omitempty"`
	MessageID  string `json:"message_id,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Text       string `json:"text,omitempty"` // message text, tool arguments, or tool result
}

// ToolCall is one tool invocation the agent made, with its result: the
// TOOL_CALL_RESULT content for a backend tool, the scripted answer for a
// frontend tool. Result is nil while unanswered.
type ToolCall struct {
	Run      int     `json:"run"`
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Args     string  `json:"args"`
	Result   *string `json:"result,omitempty"`
	Error    string  `json:"error,omitempty"`
	Frontend bool    `json:"frontend,omitempty"` // a tool the eval declared (answered by the client)
}

// HITLRequest is one human-in-the-loop request the agent made: a pending
// frontend tool call or an interrupt, and whether a scripted response
// answered it.
type HITLRequest struct {
	Run      int             `json:"run"`
	Kind     string          `json:"kind"` // "tool" or "interrupt"
	ID       string          `json:"id"`   // tool call id or interrupt id
	Name     string          `json:"name,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Detail   string          `json:"detail,omitempty"` // tool arguments or interrupt message
	Answered bool            `json:"answered"`
	Response json.RawMessage `json:"response,omitempty"` // the tool message or resume entry sent
}

// HITL request kinds.
const (
	HITLTool      = "tool"
	HITLInterrupt = "interrupt"
)

// RecordedEvent is one raw event as read, tagged with its run.
type RecordedEvent struct {
	Run   int             `json:"run"`
	Event json.RawMessage `json:"event"`
}

// Converse drives one scripted conversation against ep and returns its
// transcript. Agent-side failures — transport errors, non-2xx responses,
// non-SSE responses, malformed or schema-invalid events, a stream that does
// not begin with RUN_STARTED or ends truncated, RUN_ERROR, the conversation
// timeout — end the conversation with Outcome Errored and RuntimeError set;
// the returned error is non-nil only when ctx itself is done, so the caller
// can abort its sweep.
func Converse(ctx context.Context, ep Endpoint, s Script) (*Transcript, error) {
	start := time.Now()
	c, err := newConversation(ep, s)
	if err != nil {
		c.t.Outcome, c.t.RuntimeError = Errored, err.Error()
		c.finish(start)
		return c.t, nil
	}
	runCtx := ctx
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	maxRuns := s.MaxRuns
	if maxRuns <= 0 {
		maxRuns = DefaultMaxRuns
	}
	c.addUser(s.Prompt)
	followUps := s.FollowUps
	for {
		run, err := c.runOnce(runCtx)
		if err != nil {
			if ctx.Err() != nil {
				c.finish(start)
				return c.t, ctx.Err()
			}
			if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				err = fmt.Errorf("conversation timed out after %s", s.Timeout)
			}
			c.t.Outcome, c.t.RuntimeError = Errored, err.Error()
			break
		}
		// The run's ending decides what the client sends next. Nothing is
		// committed — answers marked given, messages added — unless another
		// run will actually carry it.
		var send func()
		switch run.Outcome {
		case OutcomeCancel:
			c.t.Outcome = Cancelled
		case OutcomeInterrupt:
			send = c.answerInterrupts(run)
		default:
			if pending := c.pending(run); len(pending) > 0 {
				send = c.answerTools(run, pending)
			} else if len(followUps) > 0 {
				text := followUps[0]
				send = func() { c.addUser(text); followUps = followUps[1:] }
			} else {
				c.t.Outcome = Completed
			}
		}
		switch {
		case c.t.Outcome != "":
		case send == nil:
			c.t.Outcome = AwaitingInput
		case len(c.t.Runs) >= maxRuns:
			c.t.Outcome = MaxTurns
		default:
			send()
			continue
		}
		break
	}
	c.finish(start)
	return c.t, nil
}

// conversation is the client-side state of one thread.
type conversation struct {
	ep        Endpoint
	s         Script
	client    *http.Client
	t         *Transcript
	responses []*response
	declared  map[string]bool
	props     json.RawMessage

	history []Message
	state   any
	resume  []ResumeEntry
	sent    []Message // messages added since the last run

	text  map[string]*strings.Builder // streamed message content, by message id
	args  map[string]*strings.Builder // streamed tool-call arguments, by call id
	calls map[string]int              // tool call id -> index in t.ToolCalls
	texts map[string]string           // last materialized text per message id
}

// response is a Response with its patterns compiled.
type response struct {
	Response
	tool, args, interrupt, message *regexp.Regexp
	used                           bool
}

func newConversation(ep Endpoint, s Script) (*conversation, error) {
	c := &conversation{
		ep: ep, s: s, client: ep.Client,
		t:        &Transcript{Protocol: "ag-ui", ThreadID: "thread-" + rand.Text()},
		declared: map[string]bool{},
		text:     map[string]*strings.Builder{},
		args:     map[string]*strings.Builder{},
		calls:    map[string]int{},
		texts:    map[string]string{},
		state:    map[string]any{},
	}
	if c.client == nil {
		c.client = &http.Client{CheckRedirect: sameOriginRedirects}
	}
	for _, tool := range s.Tools {
		c.declared[tool.Name] = true
	}
	if len(s.State) > 0 {
		if err := json.Unmarshal(s.State, &c.state); err != nil || c.state == nil {
			return c, fmt.Errorf("initial state is not a JSON value: %s", snippet(s.State))
		}
	}
	props, err := mergeProps(ep.ForwardedProps, s.ForwardedProps)
	if err != nil {
		return c, err
	}
	c.props = props
	for i, r := range s.Responses {
		cr := &response{Response: r}
		for _, p := range []struct {
			dst  **regexp.Regexp
			expr string
			name string
		}{
			{&cr.tool, r.Tool, "tool"}, {&cr.args, r.Args, "args"},
			{&cr.interrupt, r.Interrupt, "interrupt"}, {&cr.message, r.Message, "message"},
		} {
			if p.expr == "" {
				continue
			}
			re, err := regexp.Compile(p.expr)
			if err != nil {
				return c, fmt.Errorf("responses[%d].%s: %v", i, p.name, err)
			}
			*p.dst = re
		}
		c.responses = append(c.responses, cr)
	}
	return c, nil
}

// sameOriginRedirects follows a redirect only within the request's origin:
// endpoint headers often carry credentials, and net/http forwards custom
// headers — and re-POSTs the body on a 307/308 — to whatever host a redirect
// names.
func sameOriginRedirects(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if o := via[0].URL; req.URL.Scheme != o.Scheme || req.URL.Host != o.Host {
		return errors.New("refused a redirect to another origin")
	}
	return nil
}

// finishGrace is how long the client keeps reading after the requested run
// ended, for a late RUN_ERROR, before closing a stream the agent left open.
var finishGrace = 2 * time.Second

// mergeProps overlays the eval's forwardedProps on the endpoint's: top-level
// keys of the eval's object win; a non-object on either side replaces.
func mergeProps(base, over json.RawMessage) (json.RawMessage, error) {
	if len(over) == 0 {
		if len(base) == 0 {
			return json.RawMessage("{}"), nil
		}
		return base, nil
	}
	if len(base) == 0 {
		return over, nil
	}
	var b, o map[string]json.RawMessage
	if json.Unmarshal(base, &b) != nil || json.Unmarshal(over, &o) != nil || b == nil || o == nil {
		return over, nil
	}
	for k, v := range o {
		b[k] = v
	}
	merged, err := json.Marshal(b)
	if err != nil {
		return nil, fmt.Errorf("merging forwardedProps: %w", err)
	}
	return merged, nil
}

// addUser appends a user turn to the thread.
func (c *conversation) addUser(text string) {
	c.add(Message{ID: "msg-" + rand.Text(), Role: "user", Content: jsonString(text)})
}

// add appends a client-authored message, sent with the next run.
func (c *conversation) add(m Message) {
	c.history = append(c.history, m)
	c.sent = append(c.sent, m)
}

// runOnce performs one run: POST the accumulated thread, read and validate
// the response stream, and reduce the requested run's events into the
// conversation. The returned error is the run's runtime failure, if any.
func (c *conversation) runOnce(ctx context.Context) (run *Run, err error) {
	run = &Run{Index: len(c.t.Runs) + 1, RunID: "run-" + rand.Text(), Sent: c.sent, Resume: c.resume}
	c.sent, c.resume = nil, nil
	started := time.Now()
	defer func() {
		run.Seconds = time.Since(started).Seconds()
		c.t.Runs = append(c.t.Runs, *run)
	}()

	state, err := json.Marshal(c.state)
	if err != nil {
		return run, c.reject(run, fmt.Errorf("encoding state: %w", err))
	}
	input := RunAgentInput{
		ThreadID: c.t.ThreadID, RunID: run.RunID, ProtocolVersion: ProtocolVersion,
		State: state, Messages: c.history, Tools: c.s.Tools, Context: c.s.Context,
		ForwardedProps: c.props, Resume: run.Resume,
	}
	if input.Tools == nil {
		input.Tools = []Tool{}
	}
	if input.Context == nil {
		input.Context = []Context{}
	}
	if input.Messages == nil {
		input.Messages = []Message{}
	}
	body, err := json.Marshal(input)
	if err != nil {
		return run, c.reject(run, fmt.Errorf("encoding input: %w", err))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ep.URL, bytes.NewReader(body))
	if err != nil {
		return run, c.reject(run, fmt.Errorf("endpoint URL: %w", unwrapURL(err)))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range c.ep.Headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return run, c.reject(run, fmt.Errorf("POST: %w", unwrapURL(err)))
	}
	defer resp.Body.Close()
	run.Status = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return run, c.reject(run, fmt.Errorf("HTTP %d: %s", resp.StatusCode, bodyTail(resp.Body)))
	}
	ct := resp.Header.Get("Content-Type")
	if mt, _, _ := mime.ParseMediaType(ct); mt != "text/event-stream" {
		return run, c.reject(run, fmt.Errorf("response is not an SSE stream (Content-Type %q): %s",
			ct, bodyTail(resp.Body)))
	}

	st := &stream{c: c, run: run, val: newValidator(c.t.ThreadID), closeBody: func() { _ = resp.Body.Close() }}
	readErr := st.read(resp.Body)
	st.stopGrace()
	return run, st.settle(readErr)
}

// reject records a run that failed before (or while) streaming.
func (c *conversation) reject(run *Run, err error) error {
	run.Outcome, run.Error = "rejected", err.Error()
	return err
}

// unwrapURL drops the URL net/http prefixes to its errors: an endpoint URL
// may carry an expanded secret, and transcripts never record one.
func unwrapURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// bodyTail reads a bounded tail of an error response body for diagnostics.
func bodyTail(r io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(r, 64<<10))
	s := strings.Join(strings.Fields(string(data)), " ")
	const max = 300
	if len(s) > max {
		s = "…" + s[len(s)-max:]
	}
	if s == "" {
		return "(empty body)"
	}
	return s
}

// stream consumes one response: it decodes, expands, and validates every
// event, segments them into runs, and reduces the requested run.
type stream struct {
	c        *conversation
	run      *Run
	val      *validator
	exp      expander
	seq      int
	badStart bool
	segments []*segment
	cur      *segment

	// ourEnded is set once the requested run (by echoed runId) has its
	// terminal event; a stream that then breaks or stays open loses nothing.
	ourEnded  bool
	closeBody func()
	grace     *time.Timer
	graceShut atomic.Bool // the grace timer closed the body
}

// stopGrace disarms the grace timer once reading is over.
func (st *stream) stopGrace() {
	if st.grace != nil {
		st.grace.Stop()
	}
}

// segment is one run's events within a response.
type segment struct {
	runID    string
	events   []Event
	finished *Event // RUN_FINISHED
	errored  *Event // RUN_ERROR ending the run
	late     *Event // RUN_ERROR after RUN_FINISHED
}

// read processes every event in the body. It returns a runtime error for a
// fatal event or a broken stream; violations are recorded either way.
func (st *stream) read(body io.Reader) error {
	sse := newSSEReader(body)
	for {
		data, err := sse.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		trailing := errors.Is(err, errTrailingEvent)
		if err != nil && !trailing {
			if st.ourEnded {
				// The requested run is complete; a broken or held-open tail
				// costs nothing but a note.
				msg := "stream did not end cleanly after the requested run finished: " + err.Error()
				if st.graceShut.Load() {
					msg = fmt.Sprintf("stream left open %s after the requested run finished; closed by the client",
						finishGrace)
				}
				st.event(0)
				st.violate(warnProblem(RuleTransport, "%s", msg))
				return nil
			}
			return fmt.Errorf("reading event stream: %w", err)
		}
		st.seq++
		st.run.Events++
		st.c.t.Events = append(st.c.t.Events, RecordedEvent{Run: st.run.Index, Event: rawEvent(data)})
		if trailing {
			st.violate(problem{SeverityWarning, RuleTransport, err.Error()})
		}
		ev, known, problems, fatal := decodeEvent(data)
		for _, p := range problems {
			st.violate(p)
		}
		if fatal != nil {
			st.violate(problem{SeverityError, RuleSchema, fatal.Error()})
			return fmt.Errorf("invalid event %d: %w", st.seq, fatal)
		}
		if !known {
			continue
		}
		expanded, err := st.exp.feed(ev)
		if err != nil {
			st.violate(problem{SeverityError, RuleChunk, err.Error()})
			return fmt.Errorf("invalid event %d: %w", st.seq, err)
		}
		for _, x := range expanded {
			st.accept(x)
		}
	}
	if st.val.phase == phaseActive {
		st.event(0)
		st.violate(errProblem(RuleLifecycle, "stream ended before run %s finished (truncated)", st.val.runID))
	}
	return nil
}

// violate records a problem at the current event.
func (st *stream) violate(p problem) {
	st.c.t.Violations = append(st.c.t.Violations, Violation{
		Run: st.run.Index, Event: st.seq, Severity: p.severity, Rule: p.rule, Message: p.message,
	})
}

// event repositions subsequent violations (0 = the response as a whole).
func (st *stream) event(seq int) { st.seq = seq }

// accept validates one expanded event and files it under its run.
func (st *stream) accept(e Event) {
	if len(st.segments) == 0 && st.cur == nil && e.Type != RunStarted && e.Type != RunError {
		st.badStart = true
		st.violate(errProblem(RuleLifecycle, "stream began with %s, not RUN_STARTED", e.Type))
	}
	for _, p := range st.val.check(e) {
		st.violate(p)
	}
	e.seq = st.seq
	switch e.Type {
	case RunStarted:
		st.cur = &segment{runID: e.RunID}
		st.segments = append(st.segments, st.cur)
	case RunFinished:
		if st.cur != nil && st.cur.finished == nil && st.cur.errored == nil {
			st.cur.finished = &e
			st.ended(st.cur)
		}
	case RunError:
		switch {
		case st.cur == nil:
			st.cur = &segment{errored: &e}
			st.segments = append(st.segments, st.cur)
		case st.cur.finished != nil:
			if st.cur.late == nil {
				st.cur.late = &e
			}
		case st.cur.errored == nil:
			st.cur.errored = &e
			st.ended(st.cur)
		}
	default:
		if st.cur != nil && st.cur.finished == nil && st.cur.errored == nil {
			st.cur.events = append(st.cur.events, e)
		}
	}
}

// ended notes a segment's terminal event: when it is the requested run, the
// grace timer starts — a compliant agent closes the stream right after, and
// one that holds it open is closed for it.
func (st *stream) ended(seg *segment) {
	if seg.runID != st.run.RunID || st.ourEnded {
		return
	}
	st.ourEnded = true
	if st.closeBody != nil {
		st.grace = time.AfterFunc(finishGrace, func() {
			st.graceShut.Store(true)
			st.closeBody()
		})
	}
}

// settle picks the requested run out of the response (the one echoing its
// runId, else the last), reduces it, and turns its ending into the run's
// outcome or runtime error.
func (st *stream) settle(readErr error) error {
	var ours *segment
	at := -1
	for i, seg := range st.segments {
		if seg.runID == st.run.RunID {
			ours, at = seg, i
		}
	}
	if ours == nil && len(st.segments) > 0 {
		at = len(st.segments) - 1
		ours = st.segments[at]
		if ours.runID != "" {
			st.event(0)
			st.violate(warnProblem(RuleLifecycle, "no RUN_STARTED echoed runId %s; using run %s",
				st.run.RunID, ours.runID))
		}
	}
	// The requested run must be the last in the response: earlier runs are a
	// replay of the thread, but nothing may follow it.
	for _, seg := range st.segments[at+1:] {
		st.event(0)
		st.violate(errProblem(RuleLifecycle, "run %s followed the requested run %s in the same response",
			seg.runID, st.run.RunID))
	}
	if ours != nil {
		st.c.reduceRun(st, ours)
	}
	run := st.run
	switch {
	case readErr != nil:
		run.Outcome, run.Error = "rejected", readErr.Error()
		return readErr
	case st.badStart:
		run.Outcome, run.Error = "rejected", "stream did not begin with RUN_STARTED"
		return errors.New(run.Error)
	case ours == nil:
		run.Outcome, run.Error = "truncated", "empty event stream"
		return errors.New("agent sent no events")
	case ours.errored != nil || ours.late != nil:
		e := ours.errored
		late := ""
		if e == nil {
			e, late = ours.late, " after RUN_FINISHED"
		}
		run.Outcome, run.Usage = "error", e.Usage
		run.Error = e.Message
		if e.Code != "" {
			run.Error += " (" + e.Code + ")"
		}
		return fmt.Errorf("agent reported RUN_ERROR%s: %s", late, run.Error)
	case ours.finished == nil:
		run.Outcome, run.Error = "truncated", "stream ended before RUN_FINISHED"
		return errors.New("event stream truncated before RUN_FINISHED")
	}
	f := ours.finished
	run.Outcome, run.Result, run.Usage = OutcomeSuccess, f.Result, f.Usage
	if f.Outcome != nil {
		run.Outcome = f.Outcome.Type
		run.Interrupts = f.Outcome.Interrupts
		run.PendingToolCalls = f.Outcome.PendingToolCallIDs
	}
	return nil
}

// rawEvent keeps an event's payload for events.jsonl: verbatim when it is
// valid JSON, else as a JSON string.
func rawEvent(data []byte) json.RawMessage {
	if json.Valid(data) {
		return json.RawMessage(bytes.Clone(data))
	}
	return jsonString(string(data))
}

// pending is the frontend tool calls the run left for the client: the
// outcome's pendingToolCallIds when the agent sent them, else the calls to
// declared tools this run started without a result.
func (c *conversation) pending(run *Run) []string {
	if run.PendingToolCalls != nil {
		return run.PendingToolCalls
	}
	var ids []string
	for _, tc := range c.t.ToolCalls {
		if tc.Run == run.Index && tc.Frontend && tc.Result == nil {
			ids = append(ids, tc.ID)
		}
	}
	return ids
}

// answerTools matches every pending tool call to a scripted response,
// recording each as a HIL request, and returns the step that sends the
// answers (tool messages for the next run) — or nil, leaving the
// conversation awaiting input, when any call has no matching response.
func (c *conversation) answerTools(run *Run, ids []string) (send func()) {
	first := len(c.t.HITL)
	var msgs []Message
	for _, id := range ids {
		idx, known := c.calls[id]
		tc := ToolCall{ID: id}
		if known {
			tc = c.t.ToolCalls[idx]
		}
		c.t.HITL = append(c.t.HITL, HITLRequest{Run: run.Index, Kind: HITLTool, ID: id, Name: tc.Name, Detail: tc.Args})
		r := c.take(func(r *response) bool {
			return known && r.interrupt == nil && r.tool != nil && r.tool.MatchString(tc.Name) &&
				(r.args == nil || r.args.MatchString(tc.Args))
		})
		if r == nil {
			continue
		}
		content := r.Error
		if r.Error == "" {
			content = resultText(r.Result)
		}
		msgs = append(msgs, Message{ID: "msg-" + rand.Text(), Role: "tool", ToolCallID: id,
			Content: jsonString(content), Error: r.Error})
	}
	if len(msgs) < len(ids) {
		return nil
	}
	return func() {
		for i, msg := range msgs {
			req := &c.t.HITL[first+i]
			req.Answered = true
			req.Response, _ = json.Marshal(msg)
			c.add(msg)
			tc := &c.t.ToolCalls[c.calls[msg.ToolCallID]]
			tc.Result, tc.Error = new(contentText(msg.Content)), msg.Error
		}
	}
}

// answerInterrupts matches every interrupt of the run to a scripted
// response, recording each as a HIL request, and returns the step that sends
// the resume entries — or nil when any interrupt is uncovered: the spec
// forbids a resume that leaves one unanswered.
func (c *conversation) answerInterrupts(run *Run) (send func()) {
	first := len(c.t.HITL)
	var entries []ResumeEntry
	for _, in := range run.Interrupts {
		name := ""
		if idx, found := c.calls[in.ToolCallID]; found && in.ToolCallID != "" {
			name = c.t.ToolCalls[idx].Name
		}
		c.t.HITL = append(c.t.HITL, HITLRequest{
			Run: run.Index, Kind: HITLInterrupt, ID: in.ID, Name: name, Reason: in.Reason, Detail: in.Message,
		})
		r := c.take(func(r *response) bool {
			return r.interrupt != nil && r.interrupt.MatchString(in.Reason) &&
				(r.message == nil || r.message.MatchString(in.Message)) &&
				(r.tool == nil || r.tool.MatchString(name))
		})
		if r == nil {
			continue
		}
		status := r.Status
		if status == "" {
			status = ResumeResolved
		}
		entries = append(entries, ResumeEntry{InterruptID: in.ID, Status: status, Payload: r.Payload})
	}
	if len(entries) == 0 || len(entries) < len(run.Interrupts) {
		return nil
	}
	return func() {
		for i, entry := range entries {
			req := &c.t.HITL[first+i]
			req.Answered = true
			req.Response, _ = json.Marshal(entry)
		}
		c.resume = entries
	}
}

// take returns and consumes the first response that is still available
// (unused, or Repeat) and satisfies ok. Consumption is final even when the
// request batch ends up unanswered: the conversation stops there anyway.
func (c *conversation) take(ok func(*response) bool) *response {
	for _, r := range c.responses {
		if (!r.used || r.Repeat) && ok(r) {
			r.used = true
			return r
		}
	}
	return nil
}

// resultText renders a scripted tool result as message content: a JSON
// string verbatim, any other value compactly serialized.
func resultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return string(raw)
	}
	return buf.String()
}

// finish materializes the final transcript fields.
func (c *conversation) finish(start time.Time) {
	c.materialize()
	t := c.t
	t.RuntimeError = c.scrub(t.RuntimeError)
	for i := range t.Runs {
		t.Runs[i].Error = c.scrub(t.Runs[i].Error)
	}
	t.Messages = c.history
	if t.Messages == nil {
		t.Messages = []Message{}
	}
	t.State, _ = json.Marshal(c.state)
	var texts []string
	for _, run := range t.Runs {
		var parts []string
		for _, it := range run.Items {
			if it.Kind == "text" && it.Role == "assistant" && strings.TrimSpace(it.Text) != "" {
				parts = append(parts, it.Text)
			}
		}
		if len(parts) > 0 {
			texts = append(texts, strings.Join(parts, "\n\n"))
		}
	}
	t.Output = strings.Join(texts, "\n\n")
	t.Usage = sumUsage(t.Runs)
	t.Seconds = time.Since(start).Seconds()
}

// scrub removes the endpoint's secrets from an error message.
func (c *conversation) scrub(msg string) string {
	for _, secret := range c.ep.Secrets {
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, "[redacted]")
		}
	}
	return msg
}

// sumUsage totals the runs' reported usage per provider/model, in
// first-seen order. A count stays nil until some entry reports it.
func sumUsage(runs []Run) []TokenUsage {
	var out []TokenUsage
	index := map[string]int{}
	for _, run := range runs {
		for _, u := range run.Usage {
			key := u.Provider + "\x00" + u.Model
			i, ok := index[key]
			if !ok {
				i = len(out)
				index[key] = i
				out = append(out, TokenUsage{Provider: u.Provider, Model: u.Model})
			}
			sum := &out[i]
			for _, f := range []struct{ dst, src **int }{
				{&sum.InputTokens, &u.InputTokens}, {&sum.OutputTokens, &u.OutputTokens},
				{&sum.TotalTokens, &u.TotalTokens}, {&sum.ReasoningTokens, &u.ReasoningTokens},
				{&sum.CachedInputTokens, &u.CachedInputTokens},
				{&sum.CacheWriteInputTokens, &u.CacheWriteInputTokens},
			} {
				if *f.src == nil {
					continue
				}
				if *f.dst == nil {
					*f.dst = new(0)
				}
				**f.dst += **f.src
			}
		}
	}
	return out
}
