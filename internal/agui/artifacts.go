// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package agui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ArtifactDir is the workspace-relative directory a conversation's artifacts
// are written to. It is protocol-neutral so the judge and command assertions
// read the same files whatever protocol produced them.
const ArtifactDir = ".agent"

// The artifact file names under ArtifactDir.
const (
	EventsFile         = "events.jsonl"    // every event read, tagged with its run
	TranscriptFile     = "transcript.json" // the full Transcript
	TranscriptMarkdown = "transcript.md"   // a readable rendering for the judge
	StateFile          = "state.json"      // the final shared state
)

// WriteArtifacts materializes t under ws/ArtifactDir. Endpoint URLs and
// headers are not part of a Transcript, so nothing secret is written.
func WriteArtifacts(ws string, t *Transcript) error {
	dir := filepath.Join(ws, ArtifactDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var events bytes.Buffer
	for _, e := range t.Events {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		events.Write(line)
		events.WriteByte('\n')
	}
	transcript, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	state := []byte("{}")
	if len(t.State) > 0 {
		var buf bytes.Buffer
		if json.Indent(&buf, t.State, "", "  ") == nil {
			state = buf.Bytes()
		}
	}
	for name, data := range map[string][]byte{
		EventsFile:         events.Bytes(),
		TranscriptFile:     append(transcript, '\n'),
		TranscriptMarkdown: []byte(Markdown(t)),
		StateFile:          append(state, '\n'),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Markdown renders a transcript for a reader (and the LLM judge): each run's
// client turn, what the agent produced, and how the run ended, then the
// final shared state and any conformance violations.
func Markdown(t *Transcript) string {
	var b strings.Builder
	b.WriteString("# Agent conversation\n\n")
	fmt.Fprintf(&b, "The agent ran remotely (AG-UI). The conversation ended **%s** after %d %s (%.1fs).\n",
		t.Outcome, len(t.Runs), plural(len(t.Runs), "run", "runs"), t.Seconds)
	if t.RuntimeError != "" {
		fmt.Fprintf(&b, "\nRuntime error: %s\n", t.RuntimeError)
	}
	for _, run := range t.Runs {
		writeRun(&b, run)
	}
	if len(t.HITL) > 0 {
		b.WriteString("\n## Human-in-the-loop requests\n\n")
		for _, h := range t.HITL {
			label := h.Name
			if h.Kind == HITLInterrupt {
				label = h.Reason
			}
			answered := "unanswered"
			if h.Answered {
				answered = "answered"
			}
			fmt.Fprintf(&b, "- run %d: %s `%s` (%s) — %s\n", h.Run, h.Kind, label, h.ID, answered)
		}
	}
	b.WriteString("\n## Final shared state\n\n```json\n")
	if len(t.State) > 0 {
		var buf bytes.Buffer
		if json.Indent(&buf, t.State, "", "  ") == nil {
			b.Write(buf.Bytes())
		} else {
			b.Write(t.State)
		}
	}
	b.WriteString("\n```\n")
	if len(t.Violations) > 0 {
		b.WriteString("\n## Protocol conformance\n\n")
		for _, v := range t.Violations {
			fmt.Fprintf(&b, "- %s\n", v)
		}
	}
	return b.String()
}

// writeRun renders one run: the client's turn, what the agent produced, and
// how the run ended.
func writeRun(b *strings.Builder, run Run) {
	fmt.Fprintf(b, "\n## Run %d\n\n", run.Index)
	for _, m := range run.Sent {
		switch m.Role {
		case "tool":
			fmt.Fprintf(b, "**client → tool result** (%s):%s\n\n", m.ToolCallID, quote(m.Text()))
		default:
			fmt.Fprintf(b, "**%s:**%s\n\n", m.Role, quote(m.Text()))
		}
	}
	for _, re := range run.Resume {
		payload := ""
		if len(re.Payload) > 0 {
			payload = " with payload `" + compact(re.Payload) + "`"
		}
		fmt.Fprintf(b, "**client → resume** interrupt %s: %s%s\n\n", re.InterruptID, re.Status, payload)
	}
	for _, it := range run.Items {
		switch it.Kind {
		case itemText:
			fmt.Fprintf(b, "**%s:**%s\n\n", it.Role, quote(it.Text))
		case itemReasoning:
			fmt.Fprintf(b, "**reasoning:**%s\n\n", quote(it.Text))
		case itemToolCall:
			fmt.Fprintf(b, "**tool call** `%s` (%s) arguments: `%s`\n\n", it.Name, it.ToolCallID, it.Text)
		case itemToolResult:
			fmt.Fprintf(b, "**tool result** `%s` (%s):%s\n\n", it.Name, it.ToolCallID, quote(it.Text))
		}
	}
	fmt.Fprintf(b, "_Run %d ended: %s", run.Index, run.Outcome)
	if run.Error != "" {
		fmt.Fprintf(b, " — %s", run.Error)
	}
	b.WriteString("_\n")
	for _, in := range run.Interrupts {
		fmt.Fprintf(b, "\n- interrupt %s (%s)", in.ID, in.Reason)
		if in.Message != "" {
			fmt.Fprintf(b, ": %s", in.Message)
		}
	}
	if len(run.Interrupts) > 0 {
		b.WriteString("\n")
	}
	for _, id := range run.PendingToolCalls {
		fmt.Fprintf(b, "\n- pending tool call %s", id)
	}
	if len(run.PendingToolCalls) > 0 {
		b.WriteString("\n")
	}
}

// quote renders text as an indented block on its own lines (or " (empty)").
func quote(text string) string {
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return " (empty)"
	}
	return "\n\n> " + strings.ReplaceAll(text, "\n", "\n> ")
}

func compact(raw json.RawMessage) string {
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return string(raw)
	}
	return buf.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
