// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// agentsRepo is an agents-layout repository: support and billing share the
// local endpoint; only support defines staging (secret-bearing); billing alone
// defines eu.
func agentsRepo(t *testing.T) *Options {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{
		"support": `{"agent":{"endpoints":{
			"local":{"url":"http://localhost:8000/support"},
			"staging":{"url":"https://${SUPPORT_HOST}/agents/support","display":"Support (staging)",
			           "headers":{"Authorization":"Bearer ${SUPPORT_TOKEN}"},"forwarded_props":{"tenant":"eval"}}}},
			"evals":[{"id":"e","prompt":"p","assertions":["ok"]}]}`,
		"billing": `{"agent":{"endpoints":{
			"local":{"url":"http://localhost:8000/billing","display":"Billing (local)"},
			"eu":{"url":"https://eu.example.com/billing"}}},
			"evals":[{"id":"e","prompt":"p","assertions":["ok"]}]}`,
	} {
		path := filepath.Join(root, "agents", name, "evals.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &Options{Viper: viper.New(), Root: root}
}

func keys(t *testing.T, o *Options, endpoints ...string) []string {
	t.Helper()
	sels, err := o.HostedSelections(endpoints)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(sels))
	for i, s := range sels {
		out[i] = s.Key() + "=" + s.Model.Name
	}
	return out
}

func TestHostedSelectionsUnion(t *testing.T) {
	o := agentsRepo(t)
	// One selection per distinct endpoint, displayed by the first agent (in
	// agent order: billing, support) that names a display.
	if got := strings.Join(keys(t, o), ","); got !=
		"ag-ui/eu=eu,ag-ui/local=Billing (local),ag-ui/staging=Support (staging)" {
		t.Errorf("selections = %s", got)
	}
	if got := strings.Join(keys(t, o, "local,staging"), ","); got !=
		"ag-ui/local=Billing (local),ag-ui/staging=Support (staging)" {
		t.Errorf("--endpoint local,staging = %s", got)
	}
	if _, err := o.HostedSelections([]string{"prod"}); err == nil || !strings.Contains(err.Error(), `"prod" is not defined`) {
		t.Errorf("unknown endpoint err = %v", err)
	}
	// RunnableSelections routes an agent repository to its endpoints.
	sels, err := o.RunnableSelections("anthropic", "claude")
	if err != nil || len(sels) != 3 {
		t.Errorf("RunnableSelections = %d, %v; want the 3 endpoints", len(sels), err)
	}
}

func TestResolveAgentsLazily(t *testing.T) {
	o := agentsRepo(t)
	local, _ := o.HostedSelections([]string{"local"})
	// Only the selected endpoint resolves: staging's unset secrets are not needed.
	agents, err := o.ResolveAgents(local)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 || agents["support"]["local"].URL != "http://localhost:8000/support" ||
		agents["billing"]["local"].URL != "http://localhost:8000/billing" || len(agents["support"]) != 1 {
		t.Errorf("agents = %+v", agents)
	}

	staging, _ := o.HostedSelections([]string{"staging"})
	t.Setenv("SUPPORT_HOST", "staging.example.com")
	if _, err := o.ResolveAgents(staging); err == nil ||
		!strings.Contains(err.Error(), "agent support endpoint staging: header Authorization: environment variable "+
			"SUPPORT_TOKEN is not set") {
		t.Errorf("unset variable err = %v", err)
	}
	t.Setenv("SUPPORT_TOKEN", "s3cret")
	agents, err = o.ResolveAgents(staging)
	if err != nil {
		t.Fatal(err)
	}
	ep := agents["support"]["staging"]
	if ep.URL != "https://staging.example.com/agents/support" || ep.Headers["Authorization"] != "Bearer s3cret" ||
		string(ep.ForwardedProps) != `{"tenant":"eval"}` || agents["billing"] != nil {
		t.Errorf("staging = %+v (agents %v)", ep, agents)
	}
	// The expanded values are the endpoint's secrets, scrubbed from errors.
	if strings.Join(ep.Secrets, ",") != "staging.example.com,s3cret" {
		t.Errorf("secrets = %v", ep.Secrets)
	}
}

func TestValidateSelectionFlags(t *testing.T) {
	agents := agentsRepo(t)
	if err := agents.ValidateSelectionFlags(nil, nil, []string{"local"}); err != nil {
		t.Errorf("--endpoint in an agent repo: %v", err)
	}
	for _, tc := range []struct{ harness, models []string }{{nil, []string{"anthropic"}}, {[]string{"claude"}, nil}} {
		if err := agents.ValidateSelectionFlags(tc.harness, tc.models, nil); err == nil ||
			!strings.Contains(err.Error(), "does not apply to an agent repository") {
			t.Errorf("%v/%v in an agent repo: err = %v", tc.harness, tc.models, err)
		}
	}

	plugin := t.TempDir()
	if err := os.MkdirAll(filepath.Join(plugin, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugin, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"p","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	o := &Options{Viper: viper.New(), Root: plugin}
	if err := o.ValidateSelectionFlags(nil, nil, []string{"local"}); err == nil ||
		!strings.Contains(err.Error(), "--endpoint applies only to agent repositories") {
		t.Errorf("--endpoint in a plugin repo: err = %v", err)
	}
	if err := o.ValidateSelectionFlags([]string{"claude"}, []string{"anthropic"}, nil); err != nil {
		t.Errorf("--model/--harness in a plugin repo: %v", err)
	}
}

func TestAgentRepoKeySets(t *testing.T) {
	o := agentsRepo(t)
	// The root models/harnesses config does not apply to an agent repository.
	o.Viper.Set("models", []string{"anthropic"})
	o.Viper.Set("harnesses", []string{"claude"})
	active, configured, err := o.ActiveModelKeys()
	if err != nil || !configured || len(active) != 3 || !active["ag-ui/eu"] || !active["ag-ui/staging"] {
		t.Errorf("active = %v, configured = %v, err = %v", active, configured, err)
	}
	defined, err := o.DefinedModelKeys()
	if err != nil || strings.Join(defined, ",") != "ag-ui/eu,ag-ui/local,ag-ui/staging" {
		t.Errorf("defined = %v, %v", defined, err)
	}
	if warnings, err := o.UnsupportedModelWarnings(); err != nil || warnings != nil {
		t.Errorf("warnings = %v, %v; want none in an agent repo", warnings, err)
	}
	if err := o.ValidateFilterRestrictions(nil, []string{"openai"}); err != nil {
		t.Errorf("ValidateFilterRestrictions in an agent repo: %v", err)
	}
}

func TestJudgeRejectsHostedEndpoint(t *testing.T) {
	o := &Options{Viper: viper.New()}
	if _, err := o.JudgeSelection("ag-ui/local"); err == nil || !strings.Contains(err.Error(), "cannot be the judge") {
		t.Errorf("err = %v, want a hosted-judge rejection", err)
	}
}
