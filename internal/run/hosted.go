// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/bitwise-media-group/evolve/internal/agui"
	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/grade"
	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/results"
)

// ConverseFunc drives one scripted conversation with a hosted AG-UI agent;
// agui.Converse is the real implementation and Options.Converse injects a
// fake.
type ConverseFunc func(ctx context.Context, ep agui.Endpoint, s agui.Script) (*agui.Transcript, error)

// hostedProtocol runs one eval's conversation over a hosted-agent protocol
// and maps its record onto the common agent session.
type hostedProtocol func(ctx context.Context, h *hostedSession, ws string, c evalspec.Eval,
	timeout time.Duration, maxTurns int) (agentSession, error)

// hostedProtocols is the protocol table: every protocol evalspec.Protocols
// accepts has a driver here.
var hostedProtocols = map[string]hostedProtocol{evalspec.ProtocolAGUI: runAGUI}

// judgeContext tells the LLM judge that the agent under evaluation ran
// remotely, so the workspace holds its transcript rather than its edits.
const judgeContext = "The agent under evaluation ran remotely, behind an AG-UI endpoint: it could not edit this " +
	"workspace. Its whole conversation is in .agent/transcript.md (readable) and .agent/transcript.json, the final " +
	"shared state in .agent/state.json, and every protocol event in .agent/events.jsonl. Grade from those files and " +
	"the final response above."

// hostedSession drives an agent suite's evals against one resolved endpoint.
type hostedSession struct {
	opts     EvalOptions
	agent    string // the agent (eval set) name
	protocol string // the selection's harness id
	endpoint string // the endpoint name
	ep       agui.Endpoint
	drive    hostedProtocol
}

// newHostedSession binds a hosted selection to the agent's resolved endpoint
// (Options.Agents). An endpoint the agent defines but the command never
// resolved is a setup error, not a per-eval runtime failure.
func newHostedSession(opts EvalOptions, agent string, sel harness.Selection) (*hostedSession, error) {
	protocol := sel.Harness.ID()
	drive, ok := hostedProtocols[protocol]
	if !ok {
		return nil, fmt.Errorf("agent %s: protocol %q has no driver", agent, protocol)
	}
	name, ok := sel.Model.CLIModelID(protocol)
	if !ok || name == "" {
		name = sel.Model.BareID()
	}
	ep, ok := opts.Agents[agent][name]
	if !ok {
		return nil, fmt.Errorf("agent %s: endpoint %s was not resolved for this run", agent, name)
	}
	return &hostedSession{opts: opts, agent: agent, protocol: protocol, endpoint: name, ep: ep, drive: drive}, nil
}

// run drives one eval's conversation through the protocol's driver.
func (h *hostedSession) run(ctx context.Context, ws string, c evalspec.Eval, timeout time.Duration,
	maxTurns int,
) (agentSession, error) {
	return h.drive(ctx, h, ws, c, timeout, maxTurns)
}

// runAGUI drives one eval's AG-UI conversation, writes its .agent/ artifacts
// into the workspace (for command assertions, the judge, and the retained run
// log), and maps the transcript onto the common session.
func runAGUI(ctx context.Context, h *hostedSession, ws string, c evalspec.Eval, timeout time.Duration,
	maxTurns int,
) (agentSession, error) {
	ctx, span := tracer().Start(ctx, "evolve.agent.run", trace.WithAttributes(
		attribute.String("protocol", h.protocol),
		attribute.String("agent", h.agent),
		attribute.String("endpoint", h.endpoint),
		attribute.String("label", c.ID),
	))
	defer span.End()
	converse := h.opts.Converse
	if converse == nil {
		converse = agui.Converse
	}
	t, err := converse(ctx, h.ep, aguiScript(c, timeout, maxTurns))
	if err != nil {
		recordSpanErr(span, err)
		return agentSession{}, err
	}
	span.SetAttributes(attribute.Int("runs", len(t.Runs)), attribute.String("outcome", t.Outcome))
	if err := agui.WriteArtifacts(ws, t); err != nil {
		recordSpanErr(span, err)
		return agentSession{}, fmt.Errorf("writing agent artifacts: %w", err)
	}
	log, _ := os.ReadFile(filepath.Join(ws, agui.ArtifactDir, agui.EventsFile))
	sess := agentSession{
		elapsed:      time.Duration(t.Seconds * float64(time.Second)),
		log:          log,
		conversation: conversationOf(t),
	}
	if t.RuntimeError != "" {
		sess.runtimeError = t.RuntimeError
		sess.failure = fmt.Sprintf("%s endpoint %s runtime failure", h.protocol, h.endpoint)
		sess.failureDetail = t.RuntimeError
		return sess, nil
	}
	sess.output = t.Output
	sess.toolCalls = transcriptToolCalls(t)
	sess.measured = hostedMeasured(t.Usage, h.opts.Registry)
	sess.extend = func(o *grade.Options) {
		o.State = t.State
		o.HITL = transcriptHITL(t)
		o.Outcome = t.Outcome
		o.Violations = errorViolations(t)
		o.JudgeContext = judgeContext
	}
	return sess, nil
}

// aguiScript is an eval's side of the conversation in the client's terms.
func aguiScript(c evalspec.Eval, timeout time.Duration, maxTurns int) agui.Script {
	s := agui.Script{
		Prompt:         c.Prompt,
		State:          c.State,
		ForwardedProps: c.ForwardedProps,
		FollowUps:      c.FollowUps,
		MaxRuns:        maxTurns,
		Timeout:        timeout,
	}
	for _, t := range c.Tools {
		s.Tools = append(s.Tools, agui.Tool{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	for _, x := range c.Context {
		s.Context = append(s.Context, agui.Context{Description: x.Description, Value: x.Value})
	}
	for _, r := range c.Responses {
		s.Responses = append(s.Responses, agui.Response{
			Tool: r.Tool, Args: r.Args, Result: r.Result, Error: r.Error,
			Interrupt: r.Interrupt, Message: r.Message, Status: r.Status, Payload: r.Payload, Repeat: r.Repeat,
		})
	}
	return s
}

// transcriptToolCalls lifts the conversation's tool calls for tool_call and
// tool_sequence grading: a hosted conversation always reports its calls, so
// none observed is a non-nil empty slice (the assertion fails, not skips).
func transcriptToolCalls(t *agui.Transcript) []model.ToolCall {
	calls := make([]model.ToolCall, 0, len(t.ToolCalls))
	for _, tc := range t.ToolCalls {
		var input json.RawMessage
		if args := strings.TrimSpace(tc.Args); args != "" {
			if json.Valid([]byte(args)) {
				input = json.RawMessage(args)
			} else {
				input, _ = json.Marshal(args)
			}
		}
		calls = append(calls, model.ToolCall{ID: tc.ID, Name: tc.Name, Input: input, Result: tc.Result})
	}
	return calls
}

// transcriptHITL lifts the conversation's human-in-the-loop requests for
// hitl grading (non-nil: none observed is a measured zero).
func transcriptHITL(t *agui.Transcript) []model.HITLRequest {
	reqs := make([]model.HITLRequest, 0, len(t.HITL))
	for _, h := range t.HITL {
		reqs = append(reqs, model.HITLRequest{
			Kind: h.Kind, Name: h.Name, Reason: h.Reason, Detail: h.Detail, Answered: h.Answered,
		})
	}
	return reqs
}

// errorViolations renders the error-severity violations for conformance
// grading (non-nil: none is conformant).
func errorViolations(t *agui.Transcript) []string {
	out := []string{}
	for _, v := range t.Violations {
		if v.Severity == agui.SeverityError {
			out = append(out, v.String())
		}
	}
	return out
}

// maxRecordedViolations caps the violations a results entry records, so a
// pathological stream cannot bloat the committed file; events.jsonl and the
// transcript keep them all.
const maxRecordedViolations = 20

// conversationOf summarizes a transcript for the results file: runs, how it
// ended, the HIL exchanges, and the violations deduplicated by (severity,
// rule, message), first run kept.
func conversationOf(t *agui.Transcript) *results.Conversation {
	conv := &results.Conversation{Runs: len(t.Runs), Outcome: t.Outcome}
	for _, h := range t.HITL {
		name := h.Name
		if h.Kind == agui.HITLInterrupt {
			name = h.Reason
		}
		conv.HITL = append(conv.HITL, results.HITLExchange{Kind: h.Kind, Name: name, Answered: h.Answered})
	}
	seen := map[string]bool{}
	for _, v := range t.Violations {
		key := string(v.Severity) + "\x00" + v.Rule + "\x00" + v.Message
		if seen[key] || len(conv.ConformanceViolations) == maxRecordedViolations {
			continue
		}
		seen[key] = true
		conv.ConformanceViolations = append(conv.ConformanceViolations, results.ConformanceViolation{
			Severity: string(v.Severity), Rule: v.Rule, Message: v.Message, Run: v.Run,
		})
	}
	return conv
}

// hostedMeasured converts the token usage a hosted agent reported (summed per
// provider/model across runs) into measured usage. AG-UI's inputTokens
// includes cache reads and writes, so fresh input is what remains after both.
// The cost is computed only when every usage entry names a priced registry
// model; otherwise it stays unmeasured rather than partial.
func hostedMeasured(usage []agui.TokenUsage, registry []model.Model) *results.Measured {
	if len(usage) == 0 {
		return nil
	}
	if registry == nil {
		registry = model.AllModels(nil)
	}
	var total model.Usage
	var cost float64
	priced := true
	for _, u := range usage {
		var mu model.Usage
		if u.InputTokens != nil {
			fresh := max(*u.InputTokens-deref(u.CachedInputTokens)-deref(u.CacheWriteInputTokens), 0)
			mu.InputTokens = &fresh
		}
		mu.CacheReadTokens, mu.CacheCreationTokens, mu.OutputTokens =
			u.CachedInputTokens, u.CacheWriteInputTokens, u.OutputTokens
		for _, f := range []struct{ dst, src **int }{
			{&total.InputTokens, &mu.InputTokens}, {&total.CacheReadTokens, &mu.CacheReadTokens},
			{&total.CacheCreationTokens, &mu.CacheCreationTokens}, {&total.OutputTokens, &mu.OutputTokens},
		} {
			if *f.src != nil {
				*f.dst = new(deref(*f.dst) + **f.src)
			}
		}
		m, ok := usageModel(registry, u.Provider, u.Model)
		var c *float64
		if ok {
			c = model.UsageCostUSD(m, mu)
		}
		if c == nil {
			priced = false
		} else {
			cost += *c
		}
	}
	if priced {
		total.CostUSD = &cost
	}
	return measured(model.Model{}, &total)
}

// usageModel finds the registry model a usage entry names: by canonical id
// (provider/model) or by provider and bare id.
func usageModel(registry []model.Model, provider, name string) (model.Model, bool) {
	if provider == "" || name == "" {
		return model.Model{}, false
	}
	for _, m := range registry {
		if m.ID == provider+"/"+name || (m.ProviderID == provider && m.BareID() == name) {
			return m, true
		}
	}
	return model.Model{}, false
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
