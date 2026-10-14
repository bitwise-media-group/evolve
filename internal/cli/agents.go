// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/agui"
	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/layout"
)

// AgentSuite is one hosted agent's loaded suite: its eval set and agent block.
type AgentSuite struct {
	Set   layout.EvalSet
	Agent *evalspec.AgentSpec
}

// AgentSuites loads every agent suite in an agent repository, in agent order.
// A suite that fails to load, or is not an agent suite, is skipped: checks
// report it, and the engine skips it the same way.
func AgentSuites(repo *layout.Repo) ([]AgentSuite, error) {
	sets, err := repo.EvalSets()
	if err != nil {
		return nil, err
	}
	var out []AgentSuite
	for _, set := range sets {
		if !set.Agent || set.EvalsPath == "" {
			continue
		}
		f, err := evalspec.LoadEvals(set.EvalsPath)
		if err != nil || f.Agent == nil {
			continue
		}
		out = append(out, AgentSuite{Set: set, Agent: f.Agent})
	}
	return out, nil
}

// IsAgentRepo reports whether the selected repository holds hosted agents.
func (o *Options) IsAgentRepo() (bool, error) {
	repo, err := o.Repo()
	if err != nil {
		return false, err
	}
	return repo.IsAgentRepo(), nil
}

// agentRepo is IsAgentRepo for the model/harness resolvers, which also serve
// commands that run outside any repository (doctor, models): no detectable
// repository reads as a plugin context, and the commands that need one
// report the detection error themselves.
func (o *Options) agentRepo() bool {
	agents, err := o.IsAgentRepo()
	return err == nil && agents
}

// HostedSelections returns the hosted selections of an agent repository: one
// per distinct <protocol>/<endpoint> across its agents, in key order, each
// displayed by the first agent (in agent order) that names a display for it.
// endpoints narrows them to the named endpoints (empty = all); naming one no
// agent defines is an error. Each agent's own endpoint keys then gate which
// selections reach it (its suite's Restriction), so endpoint names work like
// environments shared across agents.
func (o *Options) HostedSelections(endpoints []string) ([]harness.Selection, error) {
	repo, err := o.Repo()
	if err != nil {
		return nil, err
	}
	suites, err := AgentSuites(repo)
	if err != nil {
		return nil, err
	}
	type endpointRef struct{ protocol, name, display string }
	byKey := map[string]*endpointRef{}
	defined := map[string]bool{}
	for _, s := range suites {
		for name, ep := range s.Agent.Endpoints {
			defined[name] = true
			key := evalspec.EndpointKey(s.Agent.Protocol, name)
			if ref := byKey[key]; ref != nil {
				if ref.display == "" {
					ref.display = ep.Display
				}
				continue
			}
			byKey[key] = &endpointRef{protocol: s.Agent.Protocol, name: name, display: ep.Display}
		}
	}
	want := map[string]bool{}
	for _, name := range endpoints {
		for _, n := range splitFlag(name) {
			if !defined[n] {
				return nil, fmt.Errorf("--endpoint %q is not defined by any agent (defined: %s)",
					n, strings.Join(sortedKeys(defined), ", "))
			}
			want[n] = true
		}
	}
	keys := sortedKeys(byKey)
	sels := make([]harness.Selection, 0, len(keys))
	for _, key := range keys {
		ref := byKey[key]
		if len(want) > 0 && !want[ref.name] {
			continue
		}
		sel, ok := harness.HostedSelection(ref.protocol, ref.name, ref.display)
		if !ok {
			continue // an unsupported protocol (checks reports it)
		}
		sels = append(sels, sel)
	}
	return sels, nil
}

// ResolveAgents expands the ${VAR} references of the endpoints sels select,
// for every agent that defines them, into the run's resolved endpoint map
// (agent → endpoint → URL, headers, forwarded props). Only selected endpoints
// are resolved, so a sweep never needs the secrets of one it does not run; an
// unset variable is a configuration error naming the agent, the endpoint, and
// the variable.
func (o *Options) ResolveAgents(sels []harness.Selection) (map[string]map[string]agui.Endpoint, error) {
	selected := map[string]bool{}
	for _, sel := range sels {
		if harness.IsHosted(sel.Harness) {
			selected[sel.Key()] = true
		}
	}
	if len(selected) == 0 {
		return nil, nil
	}
	repo, err := o.Repo()
	if err != nil {
		return nil, err
	}
	suites, err := AgentSuites(repo)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]agui.Endpoint{}
	for _, s := range suites {
		for _, name := range sortedKeys(s.Agent.Endpoints) {
			if !selected[evalspec.EndpointKey(s.Agent.Protocol, name)] {
				continue
			}
			ep, err := resolveEndpoint(s.Agent.Endpoints[name])
			if err != nil {
				return nil, fmt.Errorf("agent %s endpoint %s: %w", s.Set.Skill, name, err)
			}
			if out[s.Set.Skill] == nil {
				out[s.Set.Skill] = map[string]agui.Endpoint{}
			}
			out[s.Set.Skill][name] = ep
		}
	}
	return out, nil
}

// resolveEndpoint expands one endpoint's URL and header values from the
// environment. Every expanded value becomes one of the endpoint's secrets,
// which the client scrubs from any error it records.
func resolveEndpoint(e evalspec.Endpoint) (agui.Endpoint, error) {
	url, err := evalspec.ExpandEnv(e.URL, os.LookupEnv)
	if err != nil {
		return agui.Endpoint{}, fmt.Errorf("url: %w", err)
	}
	ep := agui.Endpoint{URL: url, ForwardedProps: e.ForwardedProps, Secrets: envValues(e.URL)}
	for _, h := range sortedKeys(e.Headers) {
		v, err := evalspec.ExpandEnv(e.Headers[h], os.LookupEnv)
		if err != nil {
			return agui.Endpoint{}, fmt.Errorf("header %s: %w", h, err)
		}
		if ep.Headers == nil {
			ep.Headers = map[string]string{}
		}
		ep.Headers[h] = v
		ep.Secrets = append(ep.Secrets, envValues(e.Headers[h])...)
	}
	return ep, nil
}

// envValues returns the values of the variables s references, skipping
// values too short to scrub without mangling unrelated text.
func envValues(s string) []string {
	var out []string
	for _, name := range evalspec.EnvRefs(s) {
		if v := os.Getenv(name); len(v) >= 4 {
			out = append(out, v)
		}
	}
	return out
}

// ValidateSelectionFlags enforces which selection flags apply to the
// repository's kind: --endpoint selects an agent repository's endpoints and
// is rejected in a plugin repository; --model and --harness select a plugin
// repository's models and are rejected in an agent repository.
func (o *Options) ValidateSelectionFlags(harnessFlag, modelFlag, endpointFlag []string) error {
	agents, err := o.IsAgentRepo()
	if err != nil {
		return err
	}
	if agents {
		for _, f := range []struct {
			name   string
			values []string
		}{{"--model", modelFlag}, {"--harness", harnessFlag}} {
			if len(f.values) > 0 {
				return fmt.Errorf("%s does not apply to an agent repository (select endpoints with --endpoint)", f.name)
			}
		}
		return nil
	}
	if len(endpointFlag) > 0 {
		return fmt.Errorf("--endpoint applies only to agent repositories (agents/<name>/ or agent/)")
	}
	return nil
}

// agentEndpointKeys returns every agent's endpoint keys, the agent
// repository's model matrix: the active set (stale-results handling, report
// filtering) and the strict --check denominator.
func (o *Options) agentEndpointKeys() (map[string]bool, error) {
	repo, err := o.Repo()
	if err != nil {
		return nil, err
	}
	suites, err := AgentSuites(repo)
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for _, s := range suites {
		for _, key := range s.Agent.EndpointKeys() {
			keys[key] = true
		}
	}
	return keys, nil
}

// EndpointEnvVars reports the ${VAR}s an endpoint's URL and headers reference,
// split by whether each is set — the doctor's view, which makes no live calls
// and never prints a value.
func EndpointEnvVars(e evalspec.Endpoint) (set, unset []string) {
	refs := evalspec.EnvRefs(e.URL)
	for _, h := range sortedKeys(e.Headers) {
		refs = append(refs, evalspec.EnvRefs(e.Headers[h])...)
	}
	slices.Sort(refs)
	for _, name := range slices.Compact(refs) {
		if _, ok := os.LookupEnv(name); ok {
			set = append(set, name)
		} else {
			unset = append(unset, name)
		}
	}
	return set, unset
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
