// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package evalspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/agui"
)

// ProtocolAGUI is the AG-UI protocol id, the default agent protocol.
const ProtocolAGUI = "ag-ui"

// Protocols is the closed set of hosted-agent protocols evolve can drive.
var Protocols = []string{ProtocolAGUI}

// Suite kinds: an evals file is a skill suite or an agent suite, never both.
const (
	KindSkill = "skill"
	KindAgent = "agent"
)

// AgentSpec is an agent suite's agent block: which protocol the hosted agent
// speaks and the endpoints it is served at. Its presence is what makes an
// evals file an agent suite.
type AgentSpec struct {
	// Name optionally names the agent; in the agents layout it must equal the
	// agent's directory, in the agent layout it names the agent.
	Name string `json:"name,omitempty"`
	// Protocol is the hosted-agent protocol (one of Protocols); "" loads as
	// ag-ui.
	Protocol string `json:"protocol,omitempty"`
	// Endpoints maps kebab-case endpoint names (environments such as local or
	// staging, shareable across agents) to where the agent is served.
	Endpoints map[string]Endpoint `json:"endpoints"`
}

// Endpoint is one place a hosted agent is served. ${VAR} references in URL
// and header values are expanded from the environment only when a sweep
// selects the endpoint; headers are never recorded anywhere.
type Endpoint struct {
	URL            string            `json:"url"`
	Display        string            `json:"display,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	ForwardedProps json.RawMessage   `json:"forwarded_props,omitempty"`
}

// EndpointKeys returns the agent's endpoints as sorted protocol-qualified
// keys ("ag-ui/staging") — the hosted selections' model keys.
func (a *AgentSpec) EndpointKeys() []string {
	keys := make([]string, 0, len(a.Endpoints))
	for name := range a.Endpoints {
		keys = append(keys, EndpointKey(a.Protocol, name))
	}
	sort.Strings(keys)
	return keys
}

// EndpointKey is the results/model key of one protocol endpoint.
func EndpointKey(protocol, endpoint string) string { return protocol + "/" + endpoint }

// Tool is a frontend tool an agent eval offers the agent; the agent's calls
// to it are answered from the eval's responses.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ContextEntry is one piece of application context an agent eval sends.
type ContextEntry struct {
	Description string `json:"description"`
	Value       string `json:"value"`
}

// Response is one pre-canned answer to a hosted agent's human-in-the-loop
// request. With Interrupt empty it answers a pending frontend tool call:
// Tool (a regex on the tool name) and optional Args (a regex on the JSON
// arguments) select the call, and Result (a JSON string is sent verbatim,
// any other value compactly serialized) or Error answers it. With Interrupt
// set (a regex on the interrupt's reason) it answers an interrupt: optional
// Message matches the interrupt's message and Tool the name of its linked
// tool call, and Status (resolved by default, or cancelled) plus Payload
// form the resume entry. Responses match in any order; each is used once
// unless Repeat.
type Response struct {
	Tool      string          `json:"tool,omitempty"`
	Args      string          `json:"args,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	Interrupt string          `json:"interrupt,omitempty"`
	Message   string          `json:"message,omitempty"`
	Status    string          `json:"status,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Repeat    bool            `json:"repeat,omitempty"`
}

// protocolFields lists, per protocol, the protocol-specific eval fields it
// accepts; the generic agent fields (responses, followups) work everywhere.
var protocolFields = map[string][]string{
	ProtocolAGUI: {"tools", "state", "context", "forwarded_props"},
}

// agentOnlyAssertions are the assertion types only an agent suite can grade:
// they read the hosted conversation's shared state, HIL requests, outcome,
// and protocol conformance.
var agentOnlyAssertions = []string{"state", "hitl", "outcome", "conformance"}

var (
	endpointNameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	envRefRE       = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

// EnvRefs returns the variable names s references as ${VAR}, in order.
func EnvRefs(s string) []string {
	matches := envRefRE.FindAllStringSubmatch(s, -1)
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m[1])
	}
	return names
}

// ExpandEnv replaces every ${VAR} in s with lookup(VAR). It fails on the
// first variable lookup does not resolve, naming it, and on a malformed
// reference (a "${" that does not open a valid ${NAME}).
func ExpandEnv(s string, lookup func(string) (string, bool)) (string, error) {
	if err := checkEnvSyntax(s); err != nil {
		return "", err
	}
	var missing string
	out := envRefRE.ReplaceAllStringFunc(s, func(ref string) string {
		name := ref[2 : len(ref)-1]
		v, ok := lookup(name)
		if !ok && missing == "" {
			missing = name
		}
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("environment variable %s is not set", missing)
	}
	return out, nil
}

// checkEnvSyntax rejects a "${" that is not a well-formed ${NAME} reference.
func checkEnvSyntax(s string) error {
	rest := envRefRE.ReplaceAllString(s, "")
	if strings.Contains(rest, "${") {
		return fmt.Errorf("malformed ${VAR} reference in %q (names are letters, digits, and _)", s)
	}
	return nil
}

// validateAgent returns the problems in an agent block.
func validateAgent(a *AgentSpec) []string {
	var problems []string
	if !slices.Contains(Protocols, a.Protocol) {
		problems = append(problems, fmt.Sprintf("agent.protocol %q is not supported (want one of %s)",
			a.Protocol, strings.Join(Protocols, ", ")))
	}
	if len(a.Endpoints) == 0 {
		problems = append(problems, "agent: no endpoints (define at least one under agent.endpoints)")
	}
	names := make([]string, 0, len(a.Endpoints))
	for name := range a.Endpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ep := a.Endpoints[name]
		label := "agent.endpoints." + name
		if !endpointNameRE.MatchString(name) || name == "all" {
			problems = append(problems, fmt.Sprintf("%s: endpoint names must be kebab-case (and not \"all\")", label))
		}
		problems = append(problems, validateURL(label, ep.URL)...)
		hnames := make([]string, 0, len(ep.Headers))
		for h := range ep.Headers {
			hnames = append(hnames, h)
		}
		sort.Strings(hnames)
		for _, h := range hnames {
			if strings.TrimSpace(h) == "" || strings.ContainsAny(h, " :\t\r\n") {
				problems = append(problems, fmt.Sprintf("%s.headers: invalid header name %q", label, h))
			}
			if err := checkEnvSyntax(ep.Headers[h]); err != nil {
				problems = append(problems, fmt.Sprintf("%s.headers.%s: %v", label, h, err))
			}
		}
		if ep.ForwardedProps != nil && isJSONNull(ep.ForwardedProps) {
			problems = append(problems, label+".forwarded_props: must not be null")
		}
	}
	return problems
}

// validateURL checks an endpoint URL's syntax with its ${VAR} references
// standing in as placeholders (nothing is expanded at check time).
func validateURL(label, raw string) []string {
	if raw == "" {
		return []string{label + ": missing url"}
	}
	if err := checkEnvSyntax(raw); err != nil {
		return []string{fmt.Sprintf("%s.url: %v", label, err)}
	}
	u, err := url.Parse(envRefRE.ReplaceAllString(raw, "placeholder"))
	if err != nil {
		return []string{fmt.Sprintf("%s.url: %v", label, err)}
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return []string{fmt.Sprintf("%s.url %q: want an absolute http(s) URL", label, raw)}
	}
	return nil
}

// agentFieldsSet names the agent-suite eval fields c sets.
func agentFieldsSet(c Eval) []string {
	var set []string
	for _, f := range []struct {
		name string
		on   bool
	}{
		{"responses", len(c.Responses) > 0},
		{"followups", len(c.FollowUps) > 0},
		{"tools", len(c.Tools) > 0},
		{"state", c.State != nil},
		{"context", len(c.Context) > 0},
		{"forwarded_props", c.ForwardedProps != nil},
	} {
		if f.on {
			set = append(set, f.name)
		}
	}
	return set
}

// validateAgentEval returns the problems in one agent-suite eval's
// agent-specific fields: protocol support for the protocol-specific ones,
// the frontend tools, initial state, context, and the scripted responses.
func validateAgentEval(c Eval, protocol, label string) []string {
	var problems []string
	allowed := protocolFields[protocol]
	for _, name := range agentFieldsSet(c) {
		if name != "responses" && name != "followups" && !slices.Contains(allowed, name) {
			problems = append(problems, fmt.Sprintf("%s: %s is not supported by protocol %q", label, name, protocol))
		}
	}
	tools := map[string]bool{}
	for j, tool := range c.Tools {
		tl := fmt.Sprintf("%s tools[%d]", label, j)
		switch {
		case tool.Name == "":
			problems = append(problems, tl+": missing name")
		case tools[tool.Name]:
			problems = append(problems, fmt.Sprintf("%s: duplicate tool %q", tl, tool.Name))
		}
		tools[tool.Name] = true
		if tool.Description == "" {
			problems = append(problems, tl+": missing description")
		}
		if tool.Parameters != nil && !isJSONObject(tool.Parameters) {
			problems = append(problems, tl+": parameters must be a JSON Schema object")
		}
	}
	if c.State != nil && isJSONNull(c.State) {
		problems = append(problems, label+": state must not be null")
	}
	if c.ForwardedProps != nil && isJSONNull(c.ForwardedProps) {
		problems = append(problems, label+": forwarded_props must not be null")
	}
	for j, ctx := range c.Context {
		if ctx.Description == "" {
			problems = append(problems, fmt.Sprintf("%s context[%d]: missing description", label, j))
		}
	}
	for j, f := range c.FollowUps {
		if strings.TrimSpace(f) == "" {
			problems = append(problems, fmt.Sprintf("%s followups[%d]: empty follow-up", label, j))
		}
	}
	for j, r := range c.Responses {
		problems = append(problems, validateResponse(r, fmt.Sprintf("%s responses[%d]", label, j))...)
	}
	return problems
}

// validateResponse checks one scripted response: exactly one kind (a
// frontend-tool answer or an interrupt answer), the kind's own fields only,
// and compilable patterns.
func validateResponse(r Response, label string) []string {
	var problems []string
	if r.Interrupt != "" {
		for _, f := range []struct {
			name string
			on   bool
		}{{"args", r.Args != ""}, {"result", r.Result != nil}, {"error", r.Error != ""}} {
			if f.on {
				problems = append(problems, fmt.Sprintf("%s: %s belongs to a tool response, not an interrupt response",
					label, f.name))
			}
		}
		if r.Status != "" && r.Status != agui.ResumeResolved && r.Status != agui.ResumeCancelled {
			problems = append(problems, fmt.Sprintf("%s: status %q (want resolved or cancelled)", label, r.Status))
		}
		if r.Payload != nil && isJSONNull(r.Payload) {
			problems = append(problems, label+": payload must not be null")
		}
	} else {
		if r.Tool == "" {
			problems = append(problems, label+": needs tool (a frontend-tool answer) or interrupt (an interrupt answer)")
		}
		for _, f := range []struct {
			name string
			on   bool
		}{{"message", r.Message != ""}, {"status", r.Status != ""}, {"payload", r.Payload != nil}} {
			if f.on {
				problems = append(problems, fmt.Sprintf("%s: %s belongs to an interrupt response, not a tool response",
					label, f.name))
			}
		}
		if (r.Result != nil) == (r.Error != "") {
			problems = append(problems, label+": a tool response needs exactly one of result or error")
		}
	}
	for _, p := range []struct{ name, expr string }{
		{"tool", r.Tool}, {"args", r.Args}, {"interrupt", r.Interrupt}, {"message", r.Message},
	} {
		if p.expr == "" {
			continue
		}
		if _, err := regexp.Compile(p.expr); err != nil {
			problems = append(problems, fmt.Sprintf("%s: invalid %s: %v", label, p.name, err))
		}
	}
	return problems
}

// responseWarnings flags tool responses that can never fire because their
// pattern matches none of the eval's declared tools (an agent can still list
// an undeclared call as pending, so this warns rather than fails).
func responseWarnings(c Eval, label string) []string {
	var warnings []string
	for j, r := range c.Responses {
		if r.Interrupt != "" || r.Tool == "" {
			continue
		}
		re, err := regexp.Compile(r.Tool)
		if err != nil {
			continue
		}
		matched := false
		for _, tool := range c.Tools {
			matched = matched || re.MatchString(tool.Name)
		}
		if !matched {
			warnings = append(warnings, fmt.Sprintf("%s responses[%d]: tool /%s/ matches no declared tool",
				label, j, r.Tool))
		}
	}
	return warnings
}

func isJSONNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

func isJSONObject(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '{'
}
