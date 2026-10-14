// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package agui

import (
	"encoding/json"
	"strings"
)

// ProtocolVersion is the AG-UI protocol version this client implements and
// sends on every RunAgentInput.
const ProtocolVersion = "1.0"

// The AG-UI 1.0 event types.
const (
	RunStarted              = "RUN_STARTED"
	RunFinished             = "RUN_FINISHED"
	RunError                = "RUN_ERROR"
	StepStarted             = "STEP_STARTED"
	StepFinished            = "STEP_FINISHED"
	TextMessageStart        = "TEXT_MESSAGE_START"
	TextMessageContent      = "TEXT_MESSAGE_CONTENT"
	TextMessageEnd          = "TEXT_MESSAGE_END"
	TextMessageChunk        = "TEXT_MESSAGE_CHUNK"
	ToolCallStart           = "TOOL_CALL_START"
	ToolCallArgs            = "TOOL_CALL_ARGS"
	ToolCallEnd             = "TOOL_CALL_END"
	ToolCallChunk           = "TOOL_CALL_CHUNK"
	ToolCallResult          = "TOOL_CALL_RESULT"
	StateSnapshot           = "STATE_SNAPSHOT"
	StateDelta              = "STATE_DELTA"
	MessagesSnapshot        = "MESSAGES_SNAPSHOT"
	ActivitySnapshot        = "ACTIVITY_SNAPSHOT"
	ActivityDelta           = "ACTIVITY_DELTA"
	ReasoningStart          = "REASONING_START"
	ReasoningEnd            = "REASONING_END"
	ReasoningMessageStart   = "REASONING_MESSAGE_START"
	ReasoningMessageContent = "REASONING_MESSAGE_CONTENT"
	ReasoningMessageEnd     = "REASONING_MESSAGE_END"
	ReasoningMessageChunk   = "REASONING_MESSAGE_CHUNK"
	ReasoningEncryptedValue = "REASONING_ENCRYPTED_VALUE"
	SubagentStarted         = "SUBAGENT_STARTED"
	SubagentFinished        = "SUBAGENT_FINISHED"
	SubagentError           = "SUBAGENT_ERROR"
	Raw                     = "RAW"
	Custom                  = "CUSTOM"
)

// RunAgentInput is the body of one run request. State, Tools, Context, and
// ForwardedProps are always sent — as {} / [] when the eval declares none —
// so pre-1.0 servers that still require them accept the input too.
type RunAgentInput struct {
	ThreadID        string          `json:"threadId"`
	RunID           string          `json:"runId"`
	ProtocolVersion string          `json:"protocolVersion,omitempty"`
	State           json.RawMessage `json:"state"`
	Messages        []Message       `json:"messages"`
	Tools           []Tool          `json:"tools"`
	Context         []Context       `json:"context"`
	ForwardedProps  json.RawMessage `json:"forwardedProps"`
	Resume          []ResumeEntry   `json:"resume,omitempty"`
}

// Message is one thread message, any role. Content is raw because its shape
// depends on the role: a string for assistant/system/developer/reasoning, a
// string or content-part array for user and tool, an object for activity.
type Message struct {
	ID             string            `json:"id"`
	Role           string            `json:"role"`
	Content        json.RawMessage   `json:"content,omitempty"`
	Name           string            `json:"name,omitempty"`
	ToolCalls      []MessageToolCall `json:"toolCalls,omitempty"`
	ToolCallID     string            `json:"toolCallId,omitempty"`
	Error          string            `json:"error,omitempty"`
	ActivityType   string            `json:"activityType,omitempty"`
	EncryptedValue string            `json:"encryptedValue,omitempty"`
	SubagentRunID  string            `json:"subagentRunId,omitempty"`
}

// Text renders the message content as plain text: a string verbatim, the
// text parts of a content-part array joined by newlines, "" otherwise.
func (m Message) Text() string { return contentText(m.Content) }

// MessageToolCall is one tool call recorded on an assistant message.
type MessageToolCall struct {
	ID             string       `json:"id"`
	Type           string       `json:"type"`
	Function       FunctionCall `json:"function"`
	EncryptedValue string       `json:"encryptedValue,omitempty"`
}

// FunctionCall is a tool call's name and JSON-encoded arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool is a frontend tool the client offers the agent: calls to it are
// answered by the client (here, from the eval's scripted responses).
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Context is one piece of application context sent with every run.
type Context struct {
	Description string `json:"description"`
	Value       string `json:"value"`
}

// ResumeEntry answers one interrupt on the run that continues from it.
type ResumeEntry struct {
	InterruptID string          `json:"interruptId"`
	Status      string          `json:"status"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// Resume statuses.
const (
	ResumeResolved  = "resolved"
	ResumeCancelled = "cancelled"
)

// Interrupt is something a paused run needs from outside before it can
// continue (an approval, a missing value).
type Interrupt struct {
	ID             string          `json:"id"`
	Reason         string          `json:"reason"`
	Message        string          `json:"message,omitempty"`
	ToolCallID     string          `json:"toolCallId,omitempty"`
	ResponseSchema json.RawMessage `json:"responseSchema,omitempty"`
	ExpiresAt      string          `json:"expiresAt,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	SubagentRunID  string          `json:"subagentRunId,omitempty"`
}

// Outcome is why a run ended (RUN_FINISHED.outcome); an absent outcome means
// success. PendingToolCallIDs is nil when the agent did not send the list
// (the client must then derive the pending calls from the stream) and a
// non-nil empty slice when it sent an empty one.
type Outcome struct {
	Type               string      `json:"type"`
	PendingToolCallIDs []string    `json:"pendingToolCallIds,omitempty"`
	Interrupts         []Interrupt `json:"interrupts,omitempty"`
}

// Outcome types.
const (
	OutcomeSuccess   = "success"
	OutcomeInterrupt = "interrupt"
	OutcomeCancel    = "cancelled"
)

// TokenUsage is one provider/model's token counts as an agent reports them.
// InputTokens includes cache reads and writes; CachedInputTokens and
// CacheWriteInputTokens are named parts of it, never additions.
type TokenUsage struct {
	Provider              string `json:"provider,omitempty"`
	Model                 string `json:"model,omitempty"`
	InputTokens           *int   `json:"inputTokens,omitempty"`
	OutputTokens          *int   `json:"outputTokens,omitempty"`
	TotalTokens           *int   `json:"totalTokens,omitempty"`
	ReasoningTokens       *int   `json:"reasoningTokens,omitempty"`
	CachedInputTokens     *int   `json:"cachedInputTokens,omitempty"`
	CacheWriteInputTokens *int   `json:"cacheWriteInputTokens,omitempty"`
}

// Event is one decoded AG-UI event: a flat union of every event type's
// fields, populated per Type. Delta is raw because it is a string on the
// streaming events and a JSON Patch array on STATE_DELTA; Content is raw
// because it is a string or content-part array on TOOL_CALL_RESULT and an
// object on ACTIVITY_SNAPSHOT.
type Event struct {
	Type            string          `json:"type"`
	ThreadID        string          `json:"threadId,omitempty"`
	RunID           string          `json:"runId,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	Outcome         *Outcome        `json:"outcome,omitempty"`
	Usage           []TokenUsage    `json:"usage,omitempty"`
	Message         string          `json:"message,omitempty"`
	Code            string          `json:"code,omitempty"`
	StepName        string          `json:"stepName,omitempty"`
	MessageID       string          `json:"messageId,omitempty"`
	Role            string          `json:"role,omitempty"`
	Name            string          `json:"name,omitempty"`
	Delta           json.RawMessage `json:"delta,omitempty"`
	ToolCallID      string          `json:"toolCallId,omitempty"`
	ToolCallName    string          `json:"toolCallName,omitempty"`
	ParentMessageID string          `json:"parentMessageId,omitempty"`
	Content         json.RawMessage `json:"content,omitempty"`
	Snapshot        json.RawMessage `json:"snapshot,omitempty"`
	Messages        []Message       `json:"messages,omitempty"`
	ActivityType    string          `json:"activityType,omitempty"`
	Patch           json.RawMessage `json:"patch,omitempty"`
	Subtype         string          `json:"subtype,omitempty"`
	EntityID        string          `json:"entityId,omitempty"`
	EncryptedValue  string          `json:"encryptedValue,omitempty"`

	// chunked marks an explicit event synthesized from a *_CHUNK event, so
	// the validator can tell a chunk-started item from an explicit one.
	chunked bool
	// seq is the event's 1-based position in its response, for violations
	// found while reducing it.
	seq int
}

// deltaText decodes a streaming event's string delta ("" when absent).
func (e Event) deltaText() string {
	var s string
	_ = json.Unmarshal(e.Delta, &s)
	return s
}

// contentText renders raw message content as plain text: a JSON string
// verbatim, the text parts of a content-part array joined by newlines, and
// "" for anything else.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// jsonString encodes s as a JSON string value.
func jsonString(s string) json.RawMessage {
	data, _ := json.Marshal(s)
	return data
}
