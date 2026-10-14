// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package evalspec parses the authored eval definitions: triggers and evals
// files in skill-creator's envelope shape ({skill_name?, evals|triggers:
// [...]}). The eval object is a superset of skill-creator's — id (string or
// integer), prompt, expected_output, files (fixture paths), expectations —
// plus evolve's extensions: deterministic assertions, max_turns, and
// timeout_seconds. The evals file also carries a top-level
// models restriction (provider/model tokens, intersected with the root models)
// that governs both its evals and the skill's triggers. Loading normalizes the
// superset: integer ids become strings, expectations expand to llm
// assertions graded before the authored ones, and fixture paths resolve
// against the file's directory.
//
// An evals file is explicitly one of two kinds. A skill suite is the shape
// above. An agent suite ({agent, evals}) evaluates a hosted agent: its agent
// block names the protocol (ag-ui by default) and the endpoints the agent is
// served at, and its evals may script the conversation — frontend tools,
// initial state, context, forwarded props, pre-canned responses to the
// agent's human-in-the-loop requests, and follow-up turns — and use the
// agent-only assertions (state, hitl, outcome, conformance). The two kinds
// are mutually exclusive: skill_name and models are rejected beside an agent
// block, and the agent fields and assertions are rejected in a skill suite.
// Restriction gives the shared applicability plumbing one input for either
// kind: a skill suite's models, or an agent suite's endpoint keys.
package evalspec
