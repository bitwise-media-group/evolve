// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package agui

import (
	"strings"
	"testing"
)

// feed runs events (JSON with $T for the thread id and $R for the requested
// run id) through one response stream and returns the violations, the read
// error, and the settle error.
func feed(t *testing.T, events ...string) (violations []Violation, readErr, settleErr error) {
	t.Helper()
	c, err := newConversation(Endpoint{}, Script{})
	if err != nil {
		t.Fatal(err)
	}
	run := &Run{Index: 1, RunID: "run-req"}
	var body strings.Builder
	for _, e := range events {
		e = strings.ReplaceAll(e, "$T", c.t.ThreadID)
		e = strings.ReplaceAll(e, "$R", run.RunID)
		body.WriteString("data: " + e + "\n\n")
	}
	st := &stream{c: c, run: run, val: newValidator(c.t.ThreadID)}
	readErr = st.read(strings.NewReader(body.String()))
	settleErr = st.settle(readErr)
	return c.t.Violations, readErr, settleErr
}

const (
	started  = `{"type":"RUN_STARTED","threadId":"$T","runId":"$R"}`
	finished = `{"type":"RUN_FINISHED","threadId":"$T","runId":"$R"}`
)

func TestValidatorCleanStream(t *testing.T) {
	violations, readErr, settleErr := feed(t,
		started,
		`{"type":"STEP_STARTED","stepName":"plan"}`,
		`{"type":"TEXT_MESSAGE_START","messageId":"m1","role":"assistant"}`,
		`{"type":"TEXT_MESSAGE_CONTENT","messageId":"m1","delta":"Hi"}`,
		`{"type":"TEXT_MESSAGE_END","messageId":"m1"}`,
		`{"type":"TEXT_MESSAGE_START","messageId":"m1"}`, // agreeing reopen
		`{"type":"TEXT_MESSAGE_CONTENT","messageId":"m1","delta":""}`,
		`{"type":"TEXT_MESSAGE_END","messageId":"m1"}`,
		`{"type":"TOOL_CALL_CHUNK","toolCallId":"c1","toolCallName":"lookup","delta":"{\"q\":"}`,
		`{"type":"TOOL_CALL_CHUNK","delta":"1}"}`,
		`{"type":"TOOL_CALL_RESULT","messageId":"m2","toolCallId":"c1","content":"ok"}`,
		`{"type":"REASONING_START","messageId":"r1"}`,
		`{"type":"REASONING_MESSAGE_CHUNK","messageId":"r1m","delta":"think"}`,
		`{"type":"REASONING_END","messageId":"r1"}`,
		`{"type":"STATE_SNAPSHOT","snapshot":{"n":1}}`,
		`{"type":"STATE_DELTA","delta":[{"op":"replace","path":"/n","value":2}]}`,
		`{"type":"ACTIVITY_SNAPSHOT","messageId":"a1","activityType":"plan","content":{}}`,
		`{"type":"ACTIVITY_DELTA","messageId":"a1","activityType":"plan","patch":[]}`,
		`{"type":"CUSTOM","name":"x","value":null}`,
		`{"type":"RAW","event":{"any":"thing"},"timestamp":1700000000000}`,
		`{"type":"STEP_FINISHED","stepName":"plan"}`,
		`{"type":"RUN_FINISHED","threadId":"$T","runId":"$R","outcome":{"type":"success","pendingToolCallIds":[]},`+
			`"usage":[{"provider":"anthropic","model":"m","inputTokens":10,"outputTokens":2}]}`,
	)
	if readErr != nil || settleErr != nil {
		t.Fatalf("errors: read %v, settle %v", readErr, settleErr)
	}
	if len(violations) != 0 {
		t.Errorf("violations = %v, want none", violations)
	}
}

func TestValidatorRules(t *testing.T) {
	tests := []struct {
		name     string
		events   []string
		rule     string
		severity Severity
		message  string
		fatal    bool // the stream is rejected (runtime error)
	}{
		// Schema.
		{"malformed JSON", []string{started, `{not json`}, RuleSchema, SeverityError, "not a JSON object", true},
		{"no type", []string{started, `{"messageId":"m"}`}, RuleSchema, SeverityError, "no string type", true},
		{"wrong field shape", []string{started, `{"type":"TEXT_MESSAGE_START","messageId":7}`},
			RuleSchema, SeverityError, `"messageId" must be a string`, true},
		{"missing required field", []string{started, `{"type":"TOOL_CALL_START","toolCallId":"c"}`},
			RuleSchema, SeverityError, `missing required field "toolCallName"`, true},
		{"enum value", []string{started, `{"type":"TEXT_MESSAGE_START","messageId":"m","role":"tool"}`},
			RuleSchema, SeverityError, `"role" is "tool"`, true},
		{"null required field", []string{started, `{"type":"STATE_SNAPSHOT","snapshot":null}`},
			RuleSchema, SeverityError, `required field "snapshot" is null`, true},
		{"null optional field", []string{started, `{"type":"RUN_FINISHED","threadId":"$T","runId":"$R","result":null}`},
			RuleSchema, SeverityError, `optional field "result" is null`, false},
		{"malformed outcome", []string{started, `{"type":"RUN_FINISHED","threadId":"$T","runId":"$R",` +
			`"outcome":{"type":"paused"}}`}, RuleSchema, SeverityError, `outcome type "paused"`, true},
		{"usage shape", []string{started, `{"type":"RUN_FINISHED","threadId":"$T","runId":"$R",` +
			`"usage":[{"inputTokens":"ten"}]}`}, RuleSchema, SeverityError, "usage[0].inputTokens", true},
		{"usage null count", []string{started, `{"type":"RUN_FINISHED","threadId":"$T","runId":"$R",` +
			`"usage":[{"inputTokens":null}]}`}, RuleSchema, SeverityError, "usage[0].inputTokens is null", false},
		{"usage unknown field", []string{started, `{"type":"RUN_FINISHED","threadId":"$T","runId":"$R",` +
			`"usage":[{"costUsd":0.1}]}`}, RuleUnknown, SeverityWarning, "usage[0].costUsd", false},
		{"run after the requested run", []string{started, finished,
			`{"type":"RUN_STARTED","threadId":"$T","runId":"later"}`, `{"type":"RUN_ERROR","message":"boom"}`},
			RuleLifecycle, SeverityError, "followed the requested run", false},
		// Unknown and deprecated.
		{"unknown event", []string{started, `{"type":"META","metaType":"x"}`, finished},
			RuleUnknown, SeverityWarning, "unknown event type META", false},
		{"unknown field", []string{started, `{"type":"STEP_STARTED","stepName":"s","color":"red"}`,
			`{"type":"STEP_FINISHED","stepName":"s"}`, finished},
			RuleUnknown, SeverityWarning, `unknown field "color"`, false},
		{"deprecated thinking event", []string{started, `{"type":"THINKING_START"}`, finished},
			RuleDeprecate, SeverityWarning, "retired in 1.0", false},
		// Lifecycle.
		{"first event", []string{`{"type":"STEP_STARTED","stepName":"s"}`, started, finished},
			RuleLifecycle, SeverityError, "began with STEP_STARTED", true},
		{"thread id", []string{`{"type":"RUN_STARTED","threadId":"other","runId":"$R"}`,
			`{"type":"RUN_FINISHED","threadId":"other","runId":"$R"}`},
			RuleLifecycle, SeverityError, `threadId "other"`, false},
		{"run id agreement", []string{started, `{"type":"RUN_FINISHED","threadId":"$T","runId":"x"}`},
			RuleLifecycle, SeverityError, `runId "x"`, false},
		{"event after finish", []string{started, finished, `{"type":"STEP_STARTED","stepName":"s"}`},
			RuleLifecycle, SeverityError, "after the run ended", false},
		{"nested run start", []string{started, started, finished},
			RuleLifecycle, SeverityError, "still active", false},
		{"error after error", []string{started, `{"type":"RUN_ERROR","message":"a"}`,
			`{"type":"RUN_ERROR","message":"b"}`}, RuleLifecycle, SeverityError, "RUN_ERROR after RUN_ERROR", true},
		{"truncation", []string{started, `{"type":"TEXT_MESSAGE_START","messageId":"m"}`},
			RuleLifecycle, SeverityError, "truncated", true},
		// Pairing.
		{"content without start", []string{started, `{"type":"TEXT_MESSAGE_CONTENT","messageId":"m","delta":"x"}`,
			finished}, RulePairing, SeverityError, "not open", false},
		{"start while open", []string{started, `{"type":"TOOL_CALL_START","toolCallId":"c","toolCallName":"t"}`,
			`{"type":"TOOL_CALL_START","toolCallId":"c","toolCallName":"t"}`,
			`{"type":"TOOL_CALL_END","toolCallId":"c"}`, finished},
			RulePairing, SeverityError, "already open", false},
		{"disagreeing reopen", []string{started, `{"type":"TEXT_MESSAGE_START","messageId":"m"}`,
			`{"type":"TEXT_MESSAGE_END","messageId":"m"}`,
			`{"type":"TEXT_MESSAGE_START","messageId":"m","role":"user"}`,
			`{"type":"TEXT_MESSAGE_END","messageId":"m"}`, finished},
			RulePairing, SeverityError, "reopened with a different start", false},
		{"unclosed at finish", []string{started, `{"type":"REASONING_START","messageId":"r"}`, finished},
			RulePairing, SeverityError, "reasoning span r still open", false},
		{"step not open", []string{started, `{"type":"STEP_FINISHED","stepName":"s"}`, finished},
			RulePairing, SeverityError, "finished but not open", false},
		{"step unclosed", []string{started, `{"type":"STEP_STARTED","stepName":"s"}`, finished},
			RulePairing, SeverityError, `step "s" still open`, false},
		// Chunks.
		{"first chunk without id", []string{started, `{"type":"TEXT_MESSAGE_CHUNK","delta":"x"}`},
			RuleChunk, SeverityError, "has no messageId", true},
		{"first tool chunk without name", []string{started, `{"type":"TOOL_CALL_CHUNK","toolCallId":"c"}`},
			RuleChunk, SeverityError, "needs toolCallId and toolCallName", true},
		{"chunk changes an opening field", []string{started,
			`{"type":"TEXT_MESSAGE_CHUNK","messageId":"m","role":"assistant","delta":"a"}`,
			`{"type":"TEXT_MESSAGE_CHUNK","messageId":"m","role":"user","delta":"b"}`},
			RuleChunk, SeverityError, "changes role", true},
		{"chunk changes parent", []string{started,
			`{"type":"TOOL_CALL_CHUNK","toolCallId":"c","toolCallName":"t","parentMessageId":"m1"}`,
			`{"type":"TOOL_CALL_CHUNK","toolCallId":"c","parentMessageId":"m2"}`},
			RuleChunk, SeverityError, "changes parentMessageId", true},
		{"chunks continued explicitly", []string{started,
			`{"type":"TEXT_MESSAGE_START","messageId":"m"}`,
			`{"type":"TEXT_MESSAGE_CHUNK","messageId":"m","delta":"x"}`, finished},
			RuleChunk, SeverityError, "", false},
		// Outcomes.
		{"interrupt without interrupts", []string{started, `{"type":"RUN_FINISHED","threadId":"$T","runId":"$R",` +
			`"outcome":{"type":"interrupt","interrupts":[]}}`}, RuleSchema, SeverityError, "no interrupts", true},
		{"duplicate interrupt ids", []string{started, `{"type":"RUN_FINISHED","threadId":"$T","runId":"$R",` +
			`"outcome":{"type":"interrupt","interrupts":[{"id":"i","reason":"a"},{"id":"i","reason":"b"}]}}`},
			RuleOutcome, SeverityError, "not unique", false},
		{"success with interrupts", []string{started, `{"type":"RUN_FINISHED","threadId":"$T","runId":"$R",` +
			`"outcome":{"type":"success","interrupts":[{"id":"i","reason":"a"}]}}`},
			RuleOutcome, SeverityError, "success outcome carries interrupts", false},
		{"pending ids mismatch", []string{started,
			`{"type":"TOOL_CALL_START","toolCallId":"c1","toolCallName":"t"}`,
			`{"type":"TOOL_CALL_END","toolCallId":"c1"}`,
			`{"type":"RUN_FINISHED","threadId":"$T","runId":"$R","outcome":{"type":"success","pendingToolCallIds":["c2"]}}`},
			RuleOutcome, SeverityError, "pendingToolCallIds [c2]", false},
		{"cancelled with result", []string{started, `{"type":"RUN_FINISHED","threadId":"$T","runId":"$R",` +
			`"outcome":{"type":"cancelled"},"result":1}`}, RuleOutcome, SeverityWarning, "carries a result", false},
		// State, activity, transport.
		{"failed state patch", []string{started, `{"type":"STATE_DELTA","delta":[{"op":"remove","path":"/nope"}]}`,
			finished}, RuleState, SeverityWarning, "not applied", false},
		{"unknown activity", []string{started,
			`{"type":"ACTIVITY_DELTA","messageId":"a","activityType":"x","patch":[]}`, finished},
			RuleActivity, SeverityWarning, "unknown activity a", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations, readErr, settleErr := feed(t, tt.events...)
			found := false
			for _, v := range violations {
				if v.Rule == tt.rule && v.Severity == tt.severity && strings.Contains(v.Message, tt.message) {
					found = true
				}
			}
			if !found {
				t.Errorf("no %s/%s violation containing %q in %v", tt.rule, tt.severity, tt.message, violations)
			}
			if fatal := readErr != nil || settleErr != nil; fatal != tt.fatal {
				t.Errorf("fatal = %v (read %v, settle %v), want %v", fatal, readErr, settleErr, tt.fatal)
			}
		})
	}
}

func TestTrailingEventIsWarned(t *testing.T) {
	c, _ := newConversation(Endpoint{}, Script{})
	run := &Run{Index: 1, RunID: "r"}
	body := "data: " + `{"type":"RUN_STARTED","threadId":"` + c.t.ThreadID + `","runId":"r"}` + "\n\n" +
		"data: " + `{"type":"RUN_FINISHED","threadId":"` + c.t.ThreadID + `","runId":"r"}`
	st := &stream{c: c, run: run, val: newValidator(c.t.ThreadID)}
	if err := st.settle(st.read(strings.NewReader(body))); err != nil {
		t.Fatalf("a final unterminated RUN_FINISHED must still finish the run: %v", err)
	}
	if len(c.t.Violations) != 1 || c.t.Violations[0].Rule != RuleTransport {
		t.Errorf("violations = %v, want one transport warning", c.t.Violations)
	}
}

// TestChunkMaySupplyOmittedFields: a later chunk may fill in an opening field
// the first chunk omitted; only a conflicting value is fatal.
func TestChunkMaySupplyOmittedFields(t *testing.T) {
	violations, readErr, settleErr := feed(t, started,
		`{"type":"TOOL_CALL_CHUNK","toolCallId":"c","toolCallName":"t","delta":"{"}`,
		`{"type":"TOOL_CALL_CHUNK","toolCallId":"c","parentMessageId":"m1","delta":"}"}`,
		`{"type":"TEXT_MESSAGE_CHUNK","messageId":"m2","delta":"a"}`,
		`{"type":"TEXT_MESSAGE_CHUNK","messageId":"m2","name":"bot","delta":"b"}`,
		finished)
	if readErr != nil || settleErr != nil || len(violations) != 0 {
		t.Errorf("read %v, settle %v, violations %v; want a clean stream", readErr, settleErr, violations)
	}
}
