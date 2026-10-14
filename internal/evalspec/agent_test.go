// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package evalspec

import (
	"encoding/json"
	"strings"
	"testing"
)

const agentSuite = `{
	"agent": {
		"name": "support",
		"endpoints": {
			"local":   {"url": "http://localhost:8000/"},
			"staging": {"url": "https://${STAGING_HOST}/agents/support", "display": "Support (staging)",
			            "headers": {"Authorization": "Bearer ${SUPPORT_AGENT_TOKEN}"},
			            "forwarded_props": {"tenant": "eval"}}
		}
	},
	"evals": [{
		"id": "refund-requires-approval",
		"prompt": "Refund order 1234 in full.",
		"tools": [{"name": "confirm_refund", "description": "Ask the user to approve a refund",
		           "parameters": {"type": "object", "properties": {"orderId": {"type": "string"}}}}],
		"state": {"customer": "c-42"},
		"context": [{"description": "locale", "value": "en-GB"}],
		"forwarded_props": {"channel": "web"},
		"responses": [
			{"tool": "^confirm_refund$", "args": "\"orderId\":\\s*\"1234\"", "result": {"approved": true}},
			{"interrupt": "confirmation", "message": "(?i)refund", "status": "resolved", "payload": {"approved": true}}
		],
		"followups": ["Thanks, email me the receipt."],
		"assertions": [
			{"type": "outcome", "expect": "completed"},
			{"type": "hitl", "tool": "^confirm_refund$"},
			{"type": "tool_sequence", "tools": ["^confirm_refund$", "^issue_refund$"]},
			{"type": "tool_call", "tool": "^issue_refund$", "max": 1, "result": "\"status\":\\s*\"ok\""},
			{"type": "state", "pointer": "/refunds/0/orderId", "equals": "1234"},
			{"type": "conformance"},
			"Confirms the amount and that a receipt was emailed"
		]
	}]
}`

func TestLoadAgentSuite(t *testing.T) {
	path := write(t, t.TempDir(), "evals.json", agentSuite)
	f, err := LoadEvals(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind() != KindAgent || f.Agent.Protocol != ProtocolAGUI {
		t.Errorf("kind = %s, protocol = %q; want an ag-ui agent suite", f.Kind(), f.Agent.Protocol)
	}
	if got := f.Restriction(); strings.Join(got, ",") != "ag-ui/local,ag-ui/staging" {
		t.Errorf("restriction = %v", got)
	}
	c := f.Evals[0]
	if len(c.Tools) != 1 || len(c.Responses) != 2 || len(c.FollowUps) != 1 || len(c.Context) != 1 ||
		string(c.State) != `{"customer": "c-42"}` || c.Responses[0].Result == nil {
		t.Errorf("eval = %+v", c)
	}
	a := c.Assertions[3]
	if a.Max == nil || *a.Max != 1 || a.Result == "" || string(c.Assertions[4].Equals) != `"1234"` {
		t.Errorf("assertions = %+v", c.Assertions)
	}
	problems, warnings := f.Validate()
	if len(problems) != 0 || len(warnings) != 0 {
		t.Errorf("problems = %v, warnings = %v", problems, warnings)
	}
}

// TestLoadAgentSuiteFormats pins that the raw-JSON fields (state, equals,
// result, payload, parameters, forwarded_props) decode from YAML and JSONC.
func TestLoadAgentSuiteFormats(t *testing.T) {
	yaml := `
agent:
  endpoints:
    local: {url: "http://localhost:8000/", forwarded_props: {tenant: eval}}
evals:
  - id: e
    prompt: p
    tools: [{name: pick, description: d, parameters: {type: object}}]
    state: {refunds: [], n: 1}
    responses:
      - {tool: pick, result: [1, "two"]}
      - {interrupt: ".", payload: {ok: true}}
    assertions:
      - {type: state, pointer: /n, equals: 1}
`
	jsonc := `{
		// comments are allowed
		"agent": {"endpoints": {"local": {"url": "http://localhost:8000/", "forwarded_props": {"tenant": "eval"}}}},
		"evals": [{"id": "e", "prompt": "p",
			"tools": [{"name": "pick", "description": "d", "parameters": {"type": "object"}}],
			"state": {"refunds": [], "n": 1},
			"responses": [{"tool": "pick", "result": [1, "two"]}, {"interrupt": ".", "payload": {"ok": true}}],
			"assertions": [{"type": "state", "pointer": "/n", "equals": 1}],
		}],
	}`
	for name, src := range map[string]string{"evals.yaml": yaml, "evals.jsonc": jsonc} {
		f, err := LoadEvals(write(t, t.TempDir(), name, src))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		c := f.Evals[0]
		var state map[string]any
		if err := json.Unmarshal(c.State, &state); err != nil || state["n"] != 1.0 {
			t.Errorf("%s: state = %s", name, c.State)
		}
		if compactJSON(c.Responses[0].Result) != `[1,"two"]` || compactJSON(c.Responses[1].Payload) != `{"ok":true}` ||
			compactJSON(c.Assertions[0].Equals) != `1` || compactJSON(c.Tools[0].Parameters) != `{"type":"object"}` ||
			compactJSON(f.Agent.Endpoints["local"].ForwardedProps) != `{"tenant":"eval"}` {
			t.Errorf("%s: raw fields = %+v", name, c)
		}
		if problems, _ := f.Validate(); len(problems) != 0 {
			t.Errorf("%s: problems = %v", name, problems)
		}
	}
}

func compactJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func TestValidateAgentSuiteProblems(t *testing.T) {
	tests := []struct {
		name, doc, want string
	}{
		{"both kinds", `{"skill_name":"s","models":["anthropic"],"agent":{"endpoints":{"l":{"url":"http://x"}}},` +
			`"evals":[]}`, "skill_name does not belong"},
		{"models beside agent", `{"models":["anthropic"],"agent":{"endpoints":{"l":{"url":"http://x"}}},"evals":[]}`,
			"models does not belong"},
		{"protocol", `{"agent":{"protocol":"a2a","endpoints":{"l":{"url":"http://x"}}},"evals":[]}`,
			`agent.protocol "a2a" is not supported`},
		{"no endpoints", `{"agent":{"endpoints":{}},"evals":[]}`, "no endpoints"},
		{"endpoint name", `{"agent":{"endpoints":{"Staging_1":{"url":"http://x"}}},"evals":[]}`, "kebab-case"},
		{"endpoint named all", `{"agent":{"endpoints":{"all":{"url":"http://x"}}},"evals":[]}`, "kebab-case"},
		{"missing url", `{"agent":{"endpoints":{"l":{}}},"evals":[]}`, "missing url"},
		{"relative url", `{"agent":{"endpoints":{"l":{"url":"/agent"}}},"evals":[]}`, "absolute http(s) URL"},
		{"bad env ref", `{"agent":{"endpoints":{"l":{"url":"http://${HOST/"}}},"evals":[]}`, "malformed ${VAR}"},
		{"bad header ref", `{"agent":{"endpoints":{"l":{"url":"http://x","headers":{"A":"${1BAD}"}}}},"evals":[]}`,
			"headers.A: malformed"},
		{"null forwarded props", `{"agent":{"endpoints":{"l":{"url":"http://x","forwarded_props":null}}},"evals":[]}`,
			"forwarded_props: must not be null"},
		{"tool response without answer", agentEval(`"responses":[{"tool":"t"}]`), "exactly one of result or error"},
		{"tool response with both", agentEval(`"responses":[{"tool":"t","result":1,"error":"x"}]`),
			"exactly one of result or error"},
		{"response without kind", agentEval(`"responses":[{"result":1}]`), "needs tool"},
		{"interrupt response with result", agentEval(`"responses":[{"interrupt":"x","result":1}]`),
			"result belongs to a tool response"},
		{"tool response with payload", agentEval(`"responses":[{"tool":"t","result":1,"payload":{}}]`),
			"payload belongs to an interrupt response"},
		{"bad status", agentEval(`"responses":[{"interrupt":"x","status":"approved"}]`), `status "approved"`},
		{"bad response regex", agentEval(`"responses":[{"tool":"(","result":1}]`), "invalid tool"},
		{"tool without description", agentEval(`"tools":[{"name":"t"}]`), "missing description"},
		{"duplicate tool", agentEval(`"tools":[{"name":"t","description":"d"},{"name":"t","description":"d"}]`),
			"duplicate tool"},
		{"tool parameters", agentEval(`"tools":[{"name":"t","description":"d","parameters":[]}]`),
			"parameters must be a JSON Schema object"},
		{"null state", agentEval(`"state":null`), "state must not be null"},
		{"empty follow-up", agentEval(`"followups":[" "]`), "empty follow-up"},
		{"outcome expect", agentAssert(`{"type":"outcome","expect":"done"}`), `expect "done"`},
		{"state pointer", agentAssert(`{"type":"state","pointer":"refunds"}`), "invalid pointer"},
		{"state equals and pattern", agentAssert(`{"type":"state","pointer":"/a","equals":1,"pattern":"x"}`),
			"equals or pattern, not both"},
		{"hitl bounds", agentAssert(`{"type":"hitl","min":3,"max":1}`), "min 3 exceeds max 1"},
		{"hitl regex", agentAssert(`{"type":"hitl","reason":"("}`), "invalid reason"},
		{"tool_call result regex", agentAssert(`{"type":"tool_call","tool":"t","result":"("}`), "invalid result"},
		{"negative max", agentAssert(`{"type":"tool_call","tool":"t","max":-1}`), "max must be >= 0"},
		{"empty tool_sequence", agentAssert(`{"type":"tool_sequence"}`), "missing tools"},
		{"tool_sequence regex", agentAssert(`{"type":"tool_sequence","tools":["a","("]}`), "invalid tools[1]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := LoadEvals(write(t, t.TempDir(), "evals.json", tt.doc))
			if err != nil {
				t.Fatal(err)
			}
			problems, _ := f.Validate()
			if !strings.Contains(strings.Join(problems, "\n"), tt.want) {
				t.Errorf("problems = %v, want one containing %q", problems, tt.want)
			}
		})
	}
}

// agentEval is a minimal valid agent suite with extra fields on its eval.
func agentEval(fields string) string {
	return `{"agent":{"endpoints":{"local":{"url":"http://localhost:8000/"}}},"evals":[{"id":"e","prompt":"p",` +
		fields + `,"assertions":["ok"]}]}`
}

// agentAssert is a minimal valid agent suite grading one assertion.
func agentAssert(assertion string) string {
	return `{"agent":{"endpoints":{"local":{"url":"http://localhost:8000/"}}},"evals":[{"id":"e","prompt":"p",` +
		`"assertions":[` + assertion + `]}]}`
}

func TestSkillSuiteRejectsAgentItems(t *testing.T) {
	f, err := LoadEvals(write(t, t.TempDir(), "evals.json", `{"evals":[{"id":"e","prompt":"p",
		"responses":[{"tool":"t","result":1}], "followups":["more"], "state":{},
		"assertions":[{"type":"outcome","expect":"completed"},{"type":"conformance"},
			{"type":"tool_sequence","tools":["Read","Write"]},{"type":"tool_call","tool":"Write","min":2,"result":"ok"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind() != KindSkill || f.Restriction() != nil {
		t.Errorf("kind = %s, restriction = %v", f.Kind(), f.Restriction())
	}
	problems, _ := f.Validate()
	joined := strings.Join(problems, "\n")
	for _, want := range []string{
		"responses, followups, state only belong in an agent suite",
		"assertions[0]: outcome assertions only grade agent suites",
		"assertions[1]: conformance assertions only grade agent suites",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems missing %q:\n%s", want, joined)
		}
	}
	// tool_sequence and the extended tool_call work on any tool-reporting harness.
	if strings.Contains(joined, "assertions[2]") || strings.Contains(joined, "assertions[3]") {
		t.Errorf("tool_sequence/tool_call flagged in a skill suite:\n%s", joined)
	}
}

func TestResponseWarnings(t *testing.T) {
	f, err := LoadEvals(write(t, t.TempDir(), "evals.json", agentEval(
		`"tools":[{"name":"confirm","description":"d"}],"responses":[{"tool":"^confirm$","result":1},`+
			`{"tool":"^other$","result":1},{"interrupt":"x"}]`)))
	if err != nil {
		t.Fatal(err)
	}
	problems, warnings := f.Validate()
	if len(problems) != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "responses[1]: tool /^other$/") {
		t.Errorf("problems = %v, warnings = %v", problems, warnings)
	}
}

func TestExpandEnv(t *testing.T) {
	env := map[string]string{"HOST": "agents.example.com", "TOKEN": "s3cret"}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	got, err := ExpandEnv("https://${HOST}/a?t=${TOKEN}&cost=$5", lookup)
	if err != nil || got != "https://agents.example.com/a?t=s3cret&cost=$5" {
		t.Errorf("ExpandEnv = %q, %v", got, err)
	}
	if _, err := ExpandEnv("Bearer ${MISSING}", lookup); err == nil || !strings.Contains(err.Error(), "MISSING") {
		t.Errorf("missing var error = %v", err)
	}
	if _, err := ExpandEnv("${BROKEN", lookup); err == nil {
		t.Error("malformed reference: want error")
	}
	if refs := EnvRefs("${A}-${B_2}-${A}"); strings.Join(refs, ",") != "A,B_2,A" {
		t.Errorf("EnvRefs = %v", refs)
	}
}
