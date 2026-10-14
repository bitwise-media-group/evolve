// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package agui is a small client for agents hosted behind an AG-UI 1.0
// endpoint (https://docs.ag-ui.com/spec/1.0). It drives one scripted
// conversation per behavioral eval: each run POSTs a RunAgentInput and reads
// the agent's HTTP+SSE event stream, and between runs the client answers the
// agent's human-in-the-loop requests — pending frontend tool calls and
// interrupts — from pre-canned responses, then sends any scripted follow-up
// user turns. Converse returns the whole exchange as a Transcript: the
// assistant text, tool calls with their arguments and results, the HIL
// exchanges, the final shared state, the token usage the agent reported, how
// the conversation ended, and every protocol-conformance violation observed.
//
// Every event is checked against the 1.0 rules as it is read: schema shape
// (a known field with a rejected value is fatal; a null optional field is a
// violation; unknown events and fields are warnings), the run lifecycle,
// open/close pairing of messages, tool calls, reasoning and steps (keyed by
// id, agreeing reopens allowed), the chunk-expansion rules, and the outcome
// invariants. A response may replay earlier runs of the thread before the
// requested one; their events are validated but not reduced into the
// conversation.
//
// The package is a leaf: it imports only the standard library, so the eval
// engine, the grader, and the spec loader can all depend on it. The
// community Go SDK is not used — it predates 1.0 (no protocolVersion,
// cancelled outcome, pendingToolCallIds, or TOOL_CALL_CHUNK) and its SSE
// reader is lossy.
package agui
