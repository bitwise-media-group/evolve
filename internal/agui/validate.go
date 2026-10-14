// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package agui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Severity grades a conformance violation: an error fails a conformance
// assertion; a warning (unknown events or fields, a failed state patch) is
// recorded but tolerated, as the spec asks consumers to tolerate them.
type Severity string

// Violation severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Violation is one protocol-conformance problem observed in an agent's
// response stream. Run is the conversation's 1-based run (request) index and
// Event the 1-based position of the offending event in that run's response
// (0 for a problem with the response as a whole).
type Violation struct {
	Run      int      `json:"run"`
	Event    int      `json:"event,omitempty"`
	Severity Severity `json:"severity"`
	Rule     string   `json:"rule"`
	Message  string   `json:"message"`
}

// String renders the violation as one log line.
func (v Violation) String() string {
	loc := fmt.Sprintf("run %d", v.Run)
	if v.Event > 0 {
		loc += fmt.Sprintf(" event %d", v.Event)
	}
	return fmt.Sprintf("%s (%s, %s): %s", v.Rule, v.Severity, loc, v.Message)
}

// Violation rules: the families of 1.0 conformance checks.
const (
	RuleSchema    = "schema"     // field shape (fatal) and null optional fields (error)
	RuleUnknown   = "unknown"    // unknown event types and fields (warning)
	RuleLifecycle = "lifecycle"  // run start/finish ordering, ids, truncation
	RulePairing   = "pairing"    // open/close of messages, tool calls, reasoning, steps
	RuleChunk     = "chunk"      // chunk expansion rules
	RuleOutcome   = "outcome"    // RUN_FINISHED outcome invariants
	RuleState     = "state"      // a STATE_DELTA that fails to apply (warning)
	RuleActivity  = "activity"   // an ACTIVITY_DELTA for an unknown activity (warning)
	RuleTransport = "transport"  // the SSE framing itself (warning)
	RuleDeprecate = "deprecated" // a retired pre-1.0 event (warning)
)

// fieldKind is the JSON shape a known event field must have.
type fieldKind int

const (
	kString fieldKind = iota
	kInt
	kBool
	kObject
	kArray
	kAny       // any JSON value but null
	kAnyOrNull // any JSON value, null included
	kContent   // a string or an array (content parts)
)

// eventSpec is one event type's field table: required and optional fields
// with their shapes, plus closed string enums. Every event also accepts the
// envelope fields (timestamp, rawEvent, metadata) and, unless noSubagent,
// subagentRunId.
type eventSpec struct {
	required   map[string]fieldKind
	optional   map[string]fieldKind
	enums      map[string][]string
	noSubagent bool
}

// fields builds a field table: name → kind.
type fields = map[string]fieldKind

var eventSpecs = map[string]eventSpec{
	RunStarted: {
		required:   fields{"threadId": kString, "runId": kString},
		optional:   fields{"protocolVersion": kString, "parentRunId": kString, "input": kObject},
		noSubagent: true,
	},
	RunFinished: {
		required:   fields{"threadId": kString, "runId": kString},
		optional:   fields{"result": kAny, "outcome": kObject, "usage": kArray},
		noSubagent: true,
	},
	RunError: {
		required:   fields{"message": kString},
		optional:   fields{"code": kString, "usage": kArray},
		noSubagent: true,
	},
	StepStarted:  {required: fields{"stepName": kString}},
	StepFinished: {required: fields{"stepName": kString}},
	TextMessageStart: {
		required: fields{"messageId": kString},
		optional: fields{"role": kString, "name": kString},
		enums:    map[string][]string{"role": textRoles},
	},
	TextMessageContent: {required: fields{"messageId": kString, "delta": kString}},
	TextMessageEnd:     {required: fields{"messageId": kString}},
	TextMessageChunk: {
		optional: fields{"messageId": kString, "role": kString, "delta": kString, "name": kString},
		enums:    map[string][]string{"role": textRoles},
	},
	ToolCallStart: {
		required: fields{"toolCallId": kString, "toolCallName": kString},
		optional: fields{"parentMessageId": kString},
	},
	ToolCallArgs: {required: fields{"toolCallId": kString, "delta": kString}},
	ToolCallEnd:  {required: fields{"toolCallId": kString}},
	ToolCallChunk: {
		optional: fields{"toolCallId": kString, "toolCallName": kString, "parentMessageId": kString, "delta": kString},
	},
	ToolCallResult: {
		required: fields{"messageId": kString, "toolCallId": kString, "content": kContent},
		optional: fields{"role": kString},
		enums:    map[string][]string{"role": {"tool"}},
	},
	StateSnapshot: {required: fields{"snapshot": kAny}},
	StateDelta:    {required: fields{"delta": kArray}},
	MessagesSnapshot: {
		required:   fields{"messages": kArray},
		noSubagent: true,
	},
	ActivitySnapshot: {
		required: fields{"messageId": kString, "activityType": kString, "content": kObject},
		optional: fields{"replace": kBool},
	},
	ActivityDelta: {
		required: fields{"messageId": kString, "activityType": kString, "patch": kArray},
	},
	ReasoningStart: {required: fields{"messageId": kString}},
	ReasoningEnd:   {required: fields{"messageId": kString}},
	ReasoningMessageStart: {
		required: fields{"messageId": kString, "role": kString},
		enums:    map[string][]string{"role": {"reasoning"}},
	},
	ReasoningMessageContent: {required: fields{"messageId": kString, "delta": kString}},
	ReasoningMessageEnd:     {required: fields{"messageId": kString}},
	ReasoningMessageChunk:   {optional: fields{"messageId": kString, "delta": kString}},
	ReasoningEncryptedValue: {
		required: fields{"subtype": kString, "entityId": kString, "encryptedValue": kString},
		enums:    map[string][]string{"subtype": {"tool-call", "message"}},
	},
	SubagentStarted: {
		required: fields{"subagentRunId": kString, "name": kString},
		optional: fields{
			"description": kString, "parentSubagentRunId": kString,
			"parentToolCallId": kString, "parentMessageId": kString,
		},
		noSubagent: true,
	},
	SubagentFinished: {
		required:   fields{"subagentRunId": kString},
		optional:   fields{"result": kAny, "outcome": kObject},
		noSubagent: true,
	},
	SubagentError: {
		required:   fields{"subagentRunId": kString, "message": kString},
		optional:   fields{"code": kString},
		noSubagent: true,
	},
	Raw:    {required: fields{"event": kAnyOrNull}, optional: fields{"source": kString}},
	Custom: {required: fields{"name": kString, "value": kAnyOrNull}},
}

// textRoles are the roles a text message may stream as.
var textRoles = []string{"developer", "system", "assistant", "user"}

// deprecatedEvents are the pre-1.0 THINKING_* events 1.0 retired in favor of
// REASONING_*; they are flagged and otherwise ignored, like unknown events.
var deprecatedEvents = []string{
	"THINKING_START", "THINKING_END", "THINKING_TEXT_MESSAGE_START",
	"THINKING_TEXT_MESSAGE_CONTENT", "THINKING_TEXT_MESSAGE_END",
}

// envelope fields every event accepts.
var envelope = fields{"type": kString, "timestamp": kInt, "rawEvent": kAny, "metadata": kAny}

// problem is a violation without its run/event coordinates, which the
// stream fills in.
type problem struct {
	severity Severity
	rule     string
	message  string
}

func errProblem(rule, format string, args ...any) problem {
	return problem{SeverityError, rule, fmt.Sprintf(format, args...)}
}

func warnProblem(rule, format string, args ...any) problem {
	return problem{SeverityWarning, rule, fmt.Sprintf(format, args...)}
}

// decodeEvent decodes and schema-checks one event's JSON. fatal is non-nil
// when the event is malformed or a known field has a shape the schema
// rejects; known is false for an event type outside 1.0 (stripped, with a
// warning). A null optional field is an error-severity problem and decodes
// as absent; an unknown field is a warning and is ignored.
func decodeEvent(data []byte) (ev Event, known bool, problems []problem, fatal error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return Event{}, false, nil, fmt.Errorf("event is not a JSON object: %s", snippet(data))
	}
	var typ string
	if raw, ok := obj["type"]; !ok || json.Unmarshal(raw, &typ) != nil || typ == "" {
		return Event{}, false, nil, fmt.Errorf("event has no string type: %s", snippet(data))
	}
	spec, ok := eventSpecs[typ]
	if !ok {
		if slices.Contains(deprecatedEvents, typ) {
			return Event{Type: typ}, false, []problem{warnProblem(RuleDeprecate,
				"%s is retired in 1.0 (use REASONING_*); ignored", typ)}, nil
		}
		return Event{Type: typ}, false, []problem{warnProblem(RuleUnknown, "unknown event type %s; ignored", typ)}, nil
	}

	problems, fatal = checkFields(typ, spec, obj)
	if fatal != nil {
		return Event{}, false, nil, fatal
	}
	if typ == RunFinished || typ == SubagentFinished {
		if p, err := checkOutcome(typ, obj["outcome"]); err != nil {
			return Event{}, false, nil, err
		} else if p != nil {
			problems = append(problems, *p)
		}
	}
	if raw, ok := obj["usage"]; ok {
		ps, err := checkUsage(typ, raw)
		if err != nil {
			return Event{}, false, nil, err
		}
		problems = append(problems, ps...)
	}
	cleaned, _ := json.Marshal(obj)
	if err := json.Unmarshal(cleaned, &ev); err != nil {
		return Event{}, false, nil, fmt.Errorf("%s: %v", typ, err)
	}
	return ev, true, problems, nil
}

// checkFields checks every field of an event against its table, deleting
// the unknown and null-optional ones from obj so they decode as absent.
func checkFields(typ string, spec eventSpec, obj map[string]json.RawMessage) ([]problem, error) {
	var problems []problem
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	sort.Strings(names) // stable problem order
	for _, name := range names {
		raw := obj[name]
		kind, required, known := fieldOf(spec, name)
		if !known {
			problems = append(problems, warnProblem(RuleUnknown, "%s: unknown field %q; ignored", typ, name))
			delete(obj, name)
			continue
		}
		if isNull(raw) && kind != kAnyOrNull {
			if required {
				return nil, fmt.Errorf("%s: required field %q is null", typ, name)
			}
			problems = append(problems, errProblem(RuleSchema,
				"%s: optional field %q is null (omit it instead)", typ, name))
			delete(obj, name)
			continue
		}
		if !hasKind(raw, kind) {
			return nil, fmt.Errorf("%s: field %q must be %s, got %s", typ, name, kindName(kind), snippet(raw))
		}
		if allowed, ok := spec.enums[name]; ok {
			var s string
			_ = json.Unmarshal(raw, &s)
			if !slices.Contains(allowed, s) {
				return nil, fmt.Errorf("%s: field %q is %q, want one of %s", typ, name, s, strings.Join(allowed, ", "))
			}
		}
	}
	required := make([]string, 0, len(spec.required))
	for name := range spec.required {
		required = append(required, name)
	}
	sort.Strings(required)
	for _, name := range required {
		if _, ok := obj[name]; !ok {
			return nil, fmt.Errorf("%s: missing required field %q", typ, name)
		}
	}
	return problems, nil
}

// fieldOf looks a field up in an event's table (envelope included).
func fieldOf(spec eventSpec, name string) (kind fieldKind, required, known bool) {
	if k, ok := spec.required[name]; ok {
		return k, true, true
	}
	if k, ok := spec.optional[name]; ok {
		return k, false, true
	}
	if k, ok := envelope[name]; ok {
		return k, name == "type", true
	}
	if name == "subagentRunId" && !spec.noSubagent {
		return kString, false, true
	}
	return 0, false, false
}

// checkOutcome validates a RUN_FINISHED/SUBAGENT_FINISHED outcome's shape.
// A malformed outcome — including an interrupt outcome with no interrupts,
// which the schema rejects (minItems 1) and which would leave nothing to
// resume — is fatal; duplicate interrupt ids are an error-severity problem.
func checkOutcome(typ string, raw json.RawMessage) (*problem, error) {
	if raw == nil {
		return nil, nil
	}
	var o struct {
		Type               string            `json:"type"`
		PendingToolCallIDs json.RawMessage   `json:"pendingToolCallIds"`
		Interrupts         []json.RawMessage `json:"interrupts"`
		InterruptIDs       json.RawMessage   `json:"interruptIds"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, fmt.Errorf("%s: malformed outcome: %v", typ, err)
	}
	allowed := []string{OutcomeSuccess, OutcomeInterrupt, OutcomeCancel}
	if typ == SubagentFinished {
		allowed = []string{OutcomeSuccess, "suspended"}
	}
	if !slices.Contains(allowed, o.Type) {
		return nil, fmt.Errorf("%s: outcome type %q, want one of %s", typ, o.Type, strings.Join(allowed, ", "))
	}
	if typ == SubagentFinished {
		return nil, nil
	}
	if o.PendingToolCallIDs != nil {
		var ids []string
		if err := json.Unmarshal(o.PendingToolCallIDs, &ids); err != nil {
			return nil, fmt.Errorf("%s: outcome.pendingToolCallIds must be an array of strings", typ)
		}
	}
	switch o.Type {
	case OutcomeInterrupt:
		if len(o.Interrupts) == 0 {
			return nil, fmt.Errorf("%s: interrupt outcome has no interrupts", typ)
		}
		seen := map[string]bool{}
		for i, raw := range o.Interrupts {
			var in struct {
				ID     *string `json:"id"`
				Reason *string `json:"reason"`
			}
			if json.Unmarshal(raw, &in) != nil || in.ID == nil || in.Reason == nil {
				return nil, fmt.Errorf("%s: outcome.interrupts[%d] needs string id and reason", typ, i)
			}
			if seen[*in.ID] {
				p := errProblem(RuleOutcome, "interrupt id %q is not unique", *in.ID)
				return &p, nil
			}
			seen[*in.ID] = true
		}
	case OutcomeSuccess:
		if o.Interrupts != nil {
			p := errProblem(RuleOutcome, "success outcome carries interrupts")
			return &p, nil
		}
	}
	return nil, nil
}

// usageFields are TokenUsage's fields and their shapes.
var usageFields = fields{
	"provider": kString, "model": kString, "inputTokens": kInt, "outputTokens": kInt, "totalTokens": kInt,
	"reasoningTokens": kInt, "cachedInputTokens": kInt, "cacheWriteInputTokens": kInt,
}

// checkUsage validates a usage array under the decoder's field rules: a known
// field with the wrong shape is fatal, a null one an error-severity problem
// (it decodes as absent), an unknown one a warning.
func checkUsage(typ string, raw json.RawMessage) ([]problem, error) {
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("%s: usage must be an array of objects", typ)
	}
	var problems []problem
	for i, e := range entries {
		names := make([]string, 0, len(e))
		for name := range e {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			v := e[name]
			want, known := usageFields[name]
			switch {
			case !known:
				problems = append(problems, warnProblem(RuleUnknown, "%s: unknown field usage[%d].%s; ignored", typ, i, name))
			case isNull(v):
				problems = append(problems, errProblem(RuleSchema,
					"%s: optional field usage[%d].%s is null (omit it instead)", typ, i, name))
			case !hasKind(v, want):
				return nil, fmt.Errorf("%s: usage[%d].%s must be %s", typ, i, name, kindName(want))
			}
		}
	}
	return problems, nil
}

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

// hasKind reports whether raw has the JSON shape kind requires.
func hasKind(raw json.RawMessage, kind fieldKind) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	switch kind {
	case kString:
		return raw[0] == '"'
	case kInt:
		var n json.Number
		if json.Unmarshal(raw, &n) != nil {
			return false
		}
		_, err := n.Int64()
		return err == nil
	case kBool:
		return string(raw) == "true" || string(raw) == "false"
	case kObject:
		return raw[0] == '{'
	case kArray:
		return raw[0] == '['
	case kContent:
		return raw[0] == '"' || raw[0] == '['
	case kAny:
		return !isNull(raw)
	}
	return true
}

func kindName(kind fieldKind) string {
	switch kind {
	case kString:
		return "a string"
	case kInt:
		return "an integer"
	case kBool:
		return "a boolean"
	case kObject:
		return "an object"
	case kArray:
		return "an array"
	case kContent:
		return "a string or an array"
	}
	return "a non-null value"
}

// snippet renders the head of a raw payload for an error message.
func snippet(data []byte) string {
	const max = 120
	s := strings.TrimSpace(string(data))
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// expander rewrites the *_CHUNK convenience events into the explicit
// START/CONTENT/END events they stand for, as the reference client does: the
// first chunk of an item must carry its id (and, for a tool call, its name)
// and opens it; later chunks with the same or no id append; any other event
// except RAW closes the open chunked item first. A later chunk that repeats
// an opening field with a different value is fatal.
type expander struct {
	kind   string // the open chunk type ("" when none)
	id     string
	role   string
	name   string
	parent string
}

// feed expands one event; fatal reports a chunk-rule breach the stream
// cannot continue past.
func (x *expander) feed(e Event) (out []Event, fatal error) {
	switch e.Type {
	case TextMessageChunk:
		return x.text(e)
	case ToolCallChunk:
		return x.tool(e)
	case ReasoningMessageChunk:
		if x.kind == e.Type && (e.MessageID == "" || e.MessageID == x.id) {
			return x.content(e), nil
		}
		out = x.close()
		if e.MessageID == "" {
			return nil, fmt.Errorf("first REASONING_MESSAGE_CHUNK has no messageId")
		}
		*x = expander{kind: e.Type, id: e.MessageID}
		out = append(out, Event{Type: ReasoningMessageStart, MessageID: e.MessageID, Role: "reasoning", chunked: true})
		return append(out, x.content(e)...), nil
	case Raw:
		return []Event{e}, nil
	}
	return append(x.close(), e), nil
}

// text expands a TEXT_MESSAGE_CHUNK.
func (x *expander) text(e Event) (out []Event, fatal error) {
	if x.kind == e.Type && (e.MessageID == "" || e.MessageID == x.id) {
		if e.Role != "" && e.Role != x.role {
			return nil, fmt.Errorf("TEXT_MESSAGE_CHUNK for %s changes role %q to %q", x.id, x.role, e.Role)
		}
		if e.Name != "" && x.name != "" && e.Name != x.name {
			return nil, fmt.Errorf("TEXT_MESSAGE_CHUNK for %s changes name %q to %q", x.id, x.name, e.Name)
		}
		if x.name == "" {
			x.name = e.Name // a later chunk may supply what the first omitted
		}
		return x.content(e), nil
	}
	out = x.close()
	if e.MessageID == "" {
		return nil, fmt.Errorf("first TEXT_MESSAGE_CHUNK has no messageId")
	}
	role := e.Role
	if role == "" {
		role = "assistant"
	}
	*x = expander{kind: e.Type, id: e.MessageID, role: role, name: e.Name}
	out = append(out, Event{Type: TextMessageStart, MessageID: e.MessageID, Role: role, Name: e.Name, chunked: true})
	return append(out, x.content(e)...), nil
}

// tool expands a TOOL_CALL_CHUNK.
func (x *expander) tool(e Event) (out []Event, fatal error) {
	if x.kind == e.Type && (e.ToolCallID == "" || e.ToolCallID == x.id) {
		if e.ToolCallName != "" && e.ToolCallName != x.name {
			return nil, fmt.Errorf("TOOL_CALL_CHUNK for %s changes toolCallName %q to %q",
				x.id, x.name, e.ToolCallName)
		}
		if e.ParentMessageID != "" && x.parent != "" && e.ParentMessageID != x.parent {
			return nil, fmt.Errorf("TOOL_CALL_CHUNK for %s changes parentMessageId %q to %q",
				x.id, x.parent, e.ParentMessageID)
		}
		if x.parent == "" {
			x.parent = e.ParentMessageID // a later chunk may supply what the first omitted
		}
		return x.content(e), nil
	}
	out = x.close()
	if e.ToolCallID == "" || e.ToolCallName == "" {
		return nil, fmt.Errorf("first TOOL_CALL_CHUNK needs toolCallId and toolCallName")
	}
	*x = expander{kind: e.Type, id: e.ToolCallID, name: e.ToolCallName, parent: e.ParentMessageID}
	out = append(out, Event{Type: ToolCallStart, ToolCallID: e.ToolCallID, ToolCallName: e.ToolCallName,
		ParentMessageID: e.ParentMessageID, chunked: true})
	return append(out, x.content(e)...), nil
}

// content is the CONTENT/ARGS event a continuing chunk stands for (none for
// a chunk without a delta).
func (x *expander) content(e Event) []Event {
	if len(e.Delta) == 0 {
		return nil
	}
	switch x.kind {
	case TextMessageChunk:
		return []Event{{Type: TextMessageContent, MessageID: x.id, Delta: e.Delta, chunked: true}}
	case ToolCallChunk:
		return []Event{{Type: ToolCallArgs, ToolCallID: x.id, Delta: e.Delta, chunked: true}}
	}
	return []Event{{Type: ReasoningMessageContent, MessageID: x.id, Delta: e.Delta, chunked: true}}
}

// close ends the open chunked item, if any.
func (x *expander) close() []Event {
	var out []Event
	switch x.kind {
	case TextMessageChunk:
		out = []Event{{Type: TextMessageEnd, MessageID: x.id, chunked: true}}
	case ToolCallChunk:
		out = []Event{{Type: ToolCallEnd, ToolCallID: x.id, chunked: true}}
	case ReasoningMessageChunk:
		out = []Event{{Type: ReasoningMessageEnd, MessageID: x.id, chunked: true}}
	}
	*x = expander{}
	return out
}

// phase is where a response stream is in the run lifecycle.
type phase int

const (
	phaseIdle     phase = iota // before the first RUN_STARTED
	phaseActive                // inside a run
	phaseFinished              // after RUN_FINISHED
	phaseErrored               // after RUN_ERROR
)

// item is one pairable stream item (message, tool call, reasoning span or
// message), keyed by id.
type item struct {
	open    bool
	chunked bool
	role    string // message role / tool call name
	parent  string // tool call parentMessageId / message name
}

// validator checks the lifecycle and pairing rules over one response's
// expanded events. It is reset per run for the pairing state.
type validator struct {
	threadID string
	phase    phase
	runID    string

	messages   map[string]*item
	calls      map[string]*item
	spans      map[string]*item
	reasoning  map[string]*item
	steps      map[string]bool
	activities map[string]bool
	results    map[string]bool
	callOrder  []string
}

func newValidator(threadID string) *validator {
	v := &validator{threadID: threadID}
	v.reset()
	return v
}

func (v *validator) reset() {
	v.messages = map[string]*item{}
	v.calls = map[string]*item{}
	v.spans = map[string]*item{}
	v.reasoning = map[string]*item{}
	v.steps = map[string]bool{}
	v.activities = map[string]bool{}
	v.results = map[string]bool{}
	v.callOrder = nil
}

// check validates one expanded event against the stream so far.
func (v *validator) check(e Event) []problem {
	switch e.Type {
	case RunStarted:
		var ps []problem
		if v.phase == phaseActive {
			ps = append(ps, errProblem(RuleLifecycle, "RUN_STARTED %s while run %s is still active", e.RunID, v.runID))
		}
		if e.ThreadID != v.threadID {
			ps = append(ps, errProblem(RuleLifecycle, "RUN_STARTED threadId %q, want %q", e.ThreadID, v.threadID))
		}
		v.phase, v.runID = phaseActive, e.RunID
		v.reset()
		return ps
	case RunFinished:
		return v.finish(e)
	case RunError:
		if v.phase == phaseErrored {
			return []problem{errProblem(RuleLifecycle, "RUN_ERROR after RUN_ERROR")}
		}
		v.phase = phaseErrored
		return nil
	}
	if v.phase != phaseActive {
		where := "before RUN_STARTED"
		if v.phase != phaseIdle {
			where = "after the run ended"
		}
		return []problem{errProblem(RuleLifecycle, "%s %s", e.Type, where)}
	}
	return v.pair(e)
}

// finish checks a RUN_FINISHED: it must end the active run (agreeing on its
// ids), every item must be closed, and the outcome must be consistent with
// the stream.
func (v *validator) finish(e Event) []problem {
	if v.phase != phaseActive {
		v.phase = phaseFinished
		return []problem{errProblem(RuleLifecycle, "RUN_FINISHED outside an active run")}
	}
	v.phase = phaseFinished
	var ps []problem
	if e.RunID != v.runID {
		ps = append(ps, errProblem(RuleLifecycle, "RUN_FINISHED runId %q, want %q", e.RunID, v.runID))
	}
	if e.ThreadID != v.threadID {
		ps = append(ps, errProblem(RuleLifecycle, "RUN_FINISHED threadId %q, want %q", e.ThreadID, v.threadID))
	}
	for _, open := range []struct {
		what  string
		items map[string]*item
	}{
		{"message", v.messages}, {"tool call", v.calls},
		{"reasoning span", v.spans}, {"reasoning message", v.reasoning},
	} {
		for _, id := range sortedOpen(open.items) {
			ps = append(ps, errProblem(RulePairing, "%s %s still open at RUN_FINISHED", open.what, id))
		}
	}
	for _, name := range sortedTrue(v.steps) {
		ps = append(ps, errProblem(RulePairing, "step %q still open at RUN_FINISHED", name))
	}
	if e.Outcome != nil && e.Outcome.Type == OutcomeSuccess && e.Outcome.PendingToolCallIDs != nil {
		want := v.unanswered()
		got := slices.Clone(e.Outcome.PendingToolCallIDs)
		sort.Strings(got)
		if !slices.Equal(got, want) {
			ps = append(ps, errProblem(RuleOutcome, "pendingToolCallIds %v, but the unanswered calls are %v", got, want))
		}
	}
	if e.Outcome != nil && e.Outcome.Type == OutcomeCancel && len(e.Result) > 0 {
		ps = append(ps, warnProblem(RuleOutcome, "cancelled run carries a result"))
	}
	return ps
}

// unanswered is the sorted ids of the calls this run started without a
// TOOL_CALL_RESULT.
func (v *validator) unanswered() []string {
	var ids []string
	for _, id := range v.callOrder {
		if !v.results[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return slices.Compact(ids)
}

// pair applies the open/close pairing rules to one in-run event.
func (v *validator) pair(e Event) []problem {
	switch e.Type {
	case TextMessageStart:
		role := e.Role
		if role == "" {
			role = "assistant"
		}
		return start(v.messages, "message", e.MessageID, role, e.Name, e.chunked)
	case TextMessageContent:
		return cont(v.messages, "message", e.MessageID, e.Type, e.chunked)
	case TextMessageEnd:
		return end(v.messages, "message", e.MessageID, e.Type, e.chunked)
	case ToolCallStart:
		if _, seen := v.calls[e.ToolCallID]; !seen {
			v.callOrder = append(v.callOrder, e.ToolCallID)
		}
		return start(v.calls, "tool call", e.ToolCallID, e.ToolCallName, e.ParentMessageID, e.chunked)
	case ToolCallArgs:
		return cont(v.calls, "tool call", e.ToolCallID, e.Type, e.chunked)
	case ToolCallEnd:
		return end(v.calls, "tool call", e.ToolCallID, e.Type, e.chunked)
	case ToolCallResult:
		v.results[e.ToolCallID] = true
	case ReasoningStart:
		return start(v.spans, "reasoning span", e.MessageID, "", "", false)
	case ReasoningEnd:
		return end(v.spans, "reasoning span", e.MessageID, e.Type, false)
	case ReasoningMessageStart:
		return start(v.reasoning, "reasoning message", e.MessageID, "reasoning", "", e.chunked)
	case ReasoningMessageContent:
		return cont(v.reasoning, "reasoning message", e.MessageID, e.Type, e.chunked)
	case ReasoningMessageEnd:
		return end(v.reasoning, "reasoning message", e.MessageID, e.Type, e.chunked)
	case StepStarted:
		if v.steps[e.StepName] {
			return []problem{errProblem(RulePairing, "step %q started while already open", e.StepName)}
		}
		v.steps[e.StepName] = true
	case StepFinished:
		if !v.steps[e.StepName] {
			return []problem{errProblem(RulePairing, "step %q finished but not open", e.StepName)}
		}
		v.steps[e.StepName] = false
	case ActivitySnapshot:
		v.activities[e.MessageID] = true
	case ActivityDelta:
		if !v.activities[e.MessageID] {
			return []problem{warnProblem(RuleActivity, "ACTIVITY_DELTA for unknown activity %s", e.MessageID)}
		}
	}
	return nil
}

// start opens an item. Opening an open item is an error; reopening a closed
// one is allowed when it agrees with the original start (the content then
// appends), in either form.
func start(items map[string]*item, what, id, role, parent string, chunked bool) []problem {
	it, seen := items[id]
	if !seen {
		items[id] = &item{open: true, chunked: chunked, role: role, parent: parent}
		return nil
	}
	if it.open {
		return []problem{errProblem(RulePairing, "%s %s started while already open", what, id)}
	}
	it.open, it.chunked = true, chunked
	if it.role != role || (parent != "" && it.parent != parent) {
		return []problem{errProblem(RulePairing, "%s %s reopened with a different start (%s/%s, was %s/%s)",
			what, id, role, parent, it.role, it.parent)}
	}
	return nil
}

// cont checks a CONTENT/ARGS event: its item must be open, in the same form.
func cont(items map[string]*item, what, id, typ string, chunked bool) []problem {
	it, ok := items[id]
	if !ok || !it.open {
		return []problem{errProblem(RulePairing, "%s for %s %s that is not open", typ, what, id)}
	}
	if it.chunked != chunked {
		return []problem{mixedForms(what, id, chunked)}
	}
	return nil
}

// end closes an item, which must be open, in the same form.
func end(items map[string]*item, what, id, typ string, chunked bool) []problem {
	it, ok := items[id]
	if !ok || !it.open {
		return []problem{errProblem(RulePairing, "%s for %s %s that is not open", typ, what, id)}
	}
	it.open = false
	if it.chunked != chunked {
		return []problem{mixedForms(what, id, chunked)}
	}
	return nil
}

func mixedForms(what, id string, chunked bool) problem {
	if chunked {
		return errProblem(RuleChunk, "%s %s started with explicit events is continued with chunks", what, id)
	}
	return errProblem(RuleChunk, "%s %s started as chunks is continued with explicit events", what, id)
}

func sortedOpen(items map[string]*item) []string {
	var ids []string
	for id, it := range items {
		if it.open {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func sortedTrue(m map[string]bool) []string {
	var keys []string
	for k, on := range m {
		if on {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
