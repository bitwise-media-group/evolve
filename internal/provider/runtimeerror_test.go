// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import "testing"

func TestRuntimeErrorAnthropic(t *testing.T) {
	a := NewAnthropic()
	success := []byte(`{"type":"result","subtype":"success","is_error":false,"result":"done"}`)
	maxTurns := []byte(`{"type":"result","subtype":"error_max_turns","is_error":true,"result":"partial work"}`)
	errEnvelope := []byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":""}`)
	cases := []struct {
		name    string
		stdout  []byte
		exit    int
		wantErr bool
	}{
		{"empty", []byte("  \n"), 1, true},
		{"success exit 0", success, 0, false},
		{"success with non-zero exit", success, 1, false}, // a result exists — grade it, never error
		{"max-turns partial", maxTurns, 1, false},
		{"error envelope, empty result", errEnvelope, 1, true},
		{"garbage, clean exit", []byte("not json"), 0, false},
		{"garbage, non-zero exit", []byte("not json"), 1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := a.RuntimeError(c.stdout, c.exit, false); (got != "") != c.wantErr {
				t.Errorf("RuntimeError(%q, %d) = %q, wantErr=%v", c.stdout, c.exit, got, c.wantErr)
			}
		})
	}
}

func TestRuntimeErrorOpenAI(t *testing.T) {
	o := NewOpenAI()
	stream := []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"hi"}}` + "\n" +
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":2}}`)
	if got := o.RuntimeError(stream, 0, false); got != "" {
		t.Errorf("valid stream = %q, want gradable", got)
	}
	if got := o.RuntimeError([]byte(""), 1, false); got == "" {
		t.Error("empty stdout must be a runtime error")
	}
	if got := o.RuntimeError([]byte(`{"type":"error","message":"auth"}`), 1, false); got == "" {
		t.Error("no agent_message + non-zero exit must be a runtime error")
	}
}

func TestRuntimeErrorCursor(t *testing.T) {
	c := NewCursor()
	if got := c.RuntimeError([]byte(`{"result":"all done"}`), 0, false); got != "" {
		t.Errorf("valid result = %q, want gradable", got)
	}
	if got := c.RuntimeError([]byte(""), 1, false); got == "" {
		t.Error("empty stdout must be a runtime error")
	}
	if got := c.RuntimeError([]byte(`{"result":""}`), 1, false); got == "" {
		t.Error("empty result + non-zero exit must be a runtime error")
	}
}

func TestRuntimeErrorCopilot(t *testing.T) {
	c := NewCopilot()
	if got := c.RuntimeError([]byte("all done\n"), 0, false); got != "" {
		t.Errorf("text answer = %q, want gradable", got)
	}
	if got := c.RuntimeError([]byte("  \n"), 1, false); got == "" {
		t.Error("empty stdout must be a runtime error")
	}
}

func TestRuntimeErrorAntigravity(t *testing.T) {
	a := NewAntigravity()
	if got := a.RuntimeError([]byte("all done\n"), 0, false); got != "" {
		t.Errorf("text answer = %q, want gradable", got)
	}
	if got := a.RuntimeError([]byte("  \n"), 1, false); got == "" {
		t.Error("empty stdout must be a runtime error")
	}
}
