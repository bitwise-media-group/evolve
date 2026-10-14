// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import "github.com/bitwise-media-group/evolve/internal/model"

// Hosted is the descriptor harness for agents hosted behind a protocol
// endpoint (AG-UI). Inference happens on the server, so there is no CLI, no
// skill directories, and no trigger tier: the eval engine drives a hosted
// selection through the protocol client instead of a CommandSpec. It
// deliberately does not implement EvalRunner, so it can never be resolved
// as the LLM judge.
type Hosted struct {
	protocol string
	name     string
}

// hostedProtocols names each supported protocol's harness.
var hostedProtocols = map[string]string{model.HarnessAGUI: "AG-UI"}

// NewHosted returns the descriptor for protocol, or false when evolve cannot
// drive it.
func NewHosted(protocol string) (*Hosted, bool) {
	name, ok := hostedProtocols[protocol]
	if !ok {
		return nil, false
	}
	return &Hosted{protocol: protocol, name: name}, true
}

// IsHosted reports whether h drives hosted agents (and so is always
// available: there is no CLI to find).
func IsHosted(h Harness) bool {
	_, ok := h.(*Hosted)
	return ok
}

// ID is the protocol id ("ag-ui"), the hosted selections' harness.
func (h *Hosted) ID() string { return h.protocol }

// Name is the protocol's display name.
func (h *Hosted) Name() string { return h.name }

// CLI is empty: a hosted agent is reached over the network.
func (h *Hosted) CLI() []string { return nil }

// EnvKeys is empty: endpoint credentials live in each agent's headers.
func (h *Hosted) EnvKeys() []string { return nil }

// SkillDirs is empty: a hosted agent loads no skills from the workspace.
func (h *Hosted) SkillDirs() []string { return nil }

// TriggerSpec is unreachable: agents have no trigger tier.
func (h *Hosted) TriggerSpec(ws, _, _ string, _ bool) model.CommandSpec {
	return model.CommandSpec{Dir: ws}
}

// ScanLine is unreachable: agents have no trigger tier.
func (h *Hosted) ScanLine([]byte, string, string) (bool, string) { return false, "" }

// HostedSelection builds the selection that runs agents against one protocol
// endpoint: an unpriced model keyed "<protocol>/<endpoint>" (provider and
// harness both the protocol, its CLI model id the endpoint name) bound to the
// protocol's Hosted harness. display names it in reports ("" = the endpoint).
func HostedSelection(protocol, endpoint, display string) (Selection, bool) {
	h, ok := NewHosted(protocol)
	if !ok {
		return Selection{}, false
	}
	if display == "" {
		display = endpoint
	}
	return Selection{
		Model: model.Model{
			ID: protocol + "/" + endpoint, ProviderID: protocol, Name: display,
			Supported: map[string]string{protocol: endpoint}, Preferred: protocol,
		},
		Harness: h,
	}, true
}
