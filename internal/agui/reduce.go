// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package agui

import (
	"crypto/rand"
	"encoding/json"
	"strings"
)

// Item kinds.
const (
	itemText       = "text"
	itemReasoning  = "reasoning"
	itemToolCall   = "tool_call"
	itemToolResult = "tool_result"
)

// reducer folds one run's validated events into the conversation: the
// message history (rebuilt from the streamed text, tool-call, and
// tool-result events, or replaced by a MESSAGES_SNAPSHOT), the tool calls
// and their results, and the shared state.
type reducer struct {
	c      *conversation
	st     *stream
	run    *Run
	before map[string]bool   // message ids in the history when the run began
	opened map[string]bool   // message ids this run streamed into
	roles  map[string]string // role per message id this run produced
	refs   []itemRef
	seen   map[itemRef]bool
}

// itemRef names one produced item; it is resolved to its final content
// once the run is reduced.
type itemRef struct{ kind, id string }

// reduceRun reduces seg (the requested run of a response) and records what
// it produced on the run, in stream order.
func (c *conversation) reduceRun(st *stream, seg *segment) {
	r := &reducer{
		c: c, st: st, run: st.run,
		before: map[string]bool{}, opened: map[string]bool{}, roles: map[string]string{},
		seen: map[itemRef]bool{},
	}
	for _, m := range c.history {
		r.before[m.ID] = true
	}
	for _, e := range seg.events {
		r.apply(e)
	}
	c.materialize()
	r.run.Items = r.items()
}

func (r *reducer) apply(e Event) {
	c := r.c
	switch e.Type {
	case TextMessageStart:
		role := e.Role
		if role == "" {
			role = "assistant"
		}
		r.openMessage(e.MessageID, role, e.Name, itemText)
	case TextMessageContent:
		r.text(e.MessageID, itemText).WriteString(e.deltaText())
	case ReasoningMessageStart:
		r.openMessage(e.MessageID, "reasoning", "", itemReasoning)
	case ReasoningMessageContent:
		r.text(e.MessageID, itemReasoning).WriteString(e.deltaText())
	case ReasoningEncryptedValue:
		r.encrypted(e)
	case ToolCallStart:
		r.openCall(e.ToolCallID, e.ToolCallName, e.ParentMessageID)
	case ToolCallArgs:
		if c.args[e.ToolCallID] == nil {
			r.openCall(e.ToolCallID, "", "")
		}
		c.args[e.ToolCallID].WriteString(e.deltaText())
	case ToolCallResult:
		r.toolResult(e)
	case StateSnapshot:
		var v any
		if json.Unmarshal(e.Snapshot, &v) == nil {
			c.state = v
		}
	case StateDelta:
		patched, err := ApplyPatch(c.state, e.Delta)
		if err != nil {
			r.st.event(e.seq)
			r.st.violate(warnProblem(RuleState, "STATE_DELTA not applied: %v", err))
			return
		}
		c.state = patched
	case MessagesSnapshot:
		r.snapshot(e.Messages)
	}
}

// openMessage starts (or reopens, appending) a streamed message.
func (r *reducer) openMessage(id, role, name, kind string) {
	c := r.c
	i := c.find(id)
	if i < 0 {
		c.history = append(c.history, Message{ID: id, Role: role, Name: name})
	}
	if c.text[id] == nil {
		b := &strings.Builder{}
		if i >= 0 {
			b.WriteString(contentText(c.history[i].Content))
		}
		c.text[id] = b
	}
	r.opened[id] = true
	if r.roles[id] == "" {
		r.roles[id] = role
	}
	r.ref(kind, id)
}

// text is the builder a message's streamed content appends to, opening the
// message when the stream never started it (already flagged as a pairing
// violation).
func (r *reducer) text(id, kind string) *strings.Builder {
	if r.c.text[id] == nil {
		role := "assistant"
		if kind == itemReasoning {
			role = "reasoning"
		}
		r.openMessage(id, role, "", kind)
	}
	return r.c.text[id]
}

// openCall starts (or reopens) a streamed tool call, attaching it to an
// assistant message: its parentMessageId when that names one, else the
// assistant message this run is streaming, else a new one.
func (r *reducer) openCall(id, name, parent string) {
	c := r.c
	idx, seen := c.calls[id]
	if !seen {
		c.calls[id] = len(c.t.ToolCalls)
		c.t.ToolCalls = append(c.t.ToolCalls, ToolCall{Run: r.run.Index, ID: id, Name: name, Frontend: c.declared[name]})
		mi := r.parentMessage(parent)
		c.history[mi].ToolCalls = append(c.history[mi].ToolCalls,
			MessageToolCall{ID: id, Type: "function", Function: FunctionCall{Name: name}})
	}
	if c.args[id] == nil {
		b := &strings.Builder{}
		if seen {
			b.WriteString(c.t.ToolCalls[idx].Args)
		}
		c.args[id] = b
	}
	r.ref(itemToolCall, id)
}

func (r *reducer) parentMessage(parent string) int {
	c := r.c
	if parent != "" {
		if i := c.find(parent); i >= 0 && c.history[i].Role == "assistant" {
			return i
		}
	} else if n := len(c.history); n > 0 && c.history[n-1].Role == "assistant" && r.opened[c.history[n-1].ID] {
		return n - 1
	}
	id := parent
	if id == "" || c.find(id) >= 0 {
		id = "msg-" + rand.Text()
	}
	c.history = append(c.history, Message{ID: id, Role: "assistant"})
	r.opened[id] = true
	r.roles[id] = "assistant"
	return len(c.history) - 1
}

// toolResult records a backend tool's result as a tool message and on its
// call.
func (r *reducer) toolResult(e Event) {
	c := r.c
	msg := Message{ID: e.MessageID, Role: "tool", ToolCallID: e.ToolCallID, Content: e.Content}
	if i := c.find(e.MessageID); i >= 0 {
		c.history[i] = msg
	} else {
		c.history = append(c.history, msg)
	}
	if idx, ok := c.calls[e.ToolCallID]; ok {
		c.t.ToolCalls[idx].Result = new(contentText(e.Content))
	}
	r.ref(itemToolResult, e.ToolCallID)
}

// encrypted attaches a REASONING_ENCRYPTED_VALUE to its message or tool call
// so it travels back to the agent with the thread.
func (r *reducer) encrypted(e Event) {
	c := r.c
	for i := range c.history {
		m := &c.history[i]
		if e.Subtype == "message" && m.ID == e.EntityID {
			m.EncryptedValue = e.EncryptedValue
			return
		}
		for j := range m.ToolCalls {
			if e.Subtype == "tool-call" && m.ToolCalls[j].ID == e.EntityID {
				m.ToolCalls[j].EncryptedValue = e.EncryptedValue
				return
			}
		}
	}
}

// snapshot replaces the history with a MESSAGES_SNAPSHOT (activity messages
// stripped), then records whatever the run produced only through the
// snapshot: new assistant text, tool calls, and tool results.
func (r *reducer) snapshot(msgs []Message) {
	c := r.c
	c.materialize()
	c.history = c.history[:0:0]
	for _, m := range msgs {
		if m.Role != "activity" {
			c.history = append(c.history, m)
		}
	}
	for _, m := range c.history {
		if r.before[m.ID] {
			continue
		}
		switch m.Role {
		case "assistant", "reasoning":
			kind := itemText
			if m.Role == "reasoning" {
				kind = itemReasoning
			}
			c.texts[m.ID] = m.Text()
			if r.roles[m.ID] == "" {
				r.roles[m.ID] = m.Role
			}
			if m.Text() != "" {
				r.ref(kind, m.ID)
			}
			for _, tc := range m.ToolCalls {
				if _, seen := c.calls[tc.ID]; !seen {
					c.calls[tc.ID] = len(c.t.ToolCalls)
					c.t.ToolCalls = append(c.t.ToolCalls, ToolCall{Run: r.run.Index, ID: tc.ID,
						Name: tc.Function.Name, Args: tc.Function.Arguments, Frontend: c.declared[tc.Function.Name]})
				}
				r.ref(itemToolCall, tc.ID)
			}
		case "tool":
			if idx, ok := c.calls[m.ToolCallID]; ok && c.t.ToolCalls[idx].Result == nil {
				c.t.ToolCalls[idx].Result = new(m.Text())
				r.ref(itemToolResult, m.ToolCallID)
			}
		}
	}
}

func (r *reducer) ref(kind, id string) {
	ref := itemRef{kind, id}
	if !r.seen[ref] {
		r.seen[ref] = true
		r.refs = append(r.refs, ref)
	}
}

// items resolves the run's item refs to their final content.
func (r *reducer) items() []Item {
	c := r.c
	out := make([]Item, 0, len(r.refs))
	for _, ref := range r.refs {
		switch ref.kind {
		case itemText, itemReasoning:
			out = append(out, Item{Kind: ref.kind, Role: r.roles[ref.id], MessageID: ref.id, Text: c.texts[ref.id]})
		case itemToolCall:
			tc := c.t.ToolCalls[c.calls[ref.id]]
			out = append(out, Item{Kind: ref.kind, ToolCallID: tc.ID, Name: tc.Name, Text: tc.Args})
		case itemToolResult:
			it := Item{Kind: ref.kind, ToolCallID: ref.id}
			if idx, ok := c.calls[ref.id]; ok {
				tc := c.t.ToolCalls[idx]
				it.Name = tc.Name
				if tc.Result != nil {
					it.Text = *tc.Result
				}
			}
			out = append(out, it)
		}
	}
	return out
}

// materialize writes the streamed text and arguments into the history and
// the tool calls, and resets the builders.
func (c *conversation) materialize() {
	for id, b := range c.text {
		if i := c.find(id); i >= 0 {
			c.history[i].Content = jsonString(b.String())
		}
		c.texts[id] = b.String()
	}
	for id, b := range c.args {
		if idx, ok := c.calls[id]; ok {
			c.t.ToolCalls[idx].Args = b.String()
		}
		for i := range c.history {
			for j := range c.history[i].ToolCalls {
				if c.history[i].ToolCalls[j].ID == id {
					c.history[i].ToolCalls[j].Function.Arguments = b.String()
				}
			}
		}
	}
	c.text = map[string]*strings.Builder{}
	c.args = map[string]*strings.Builder{}
}

// find returns the history index of message id, or -1.
func (c *conversation) find(id string) int {
	for i := len(c.history) - 1; i >= 0; i-- {
		if c.history[i].ID == id {
			return i
		}
	}
	return -1
}
