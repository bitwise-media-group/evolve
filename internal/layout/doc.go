// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package layout detects which of the supported repository shapes a root
// directory has and enumerates its plugins (or hosted agents) and eval
// definitions. A repository holds plugins or agents, never both.
//
// The shapes, in detection precedence order:
//
//   - marketplace: .claude-plugin/marketplace.json at the root, plugins under
//     plugins/<name>/.
//   - multi: plugins/<name>/.claude-plugin/plugin.json present but no
//     marketplace manifest; marketplace checks are skipped.
//   - single: the repository root IS the plugin (.claude-plugin/plugin.json at
//     the root) with skills/ and evals/ beside it.
//   - agents: agents/<name>/evals.<ext>, one hosted agent per directory, its
//     evals beside its declarative definition (agent code, system prompt).
//   - agent: agent/evals.<ext>, the repository is one hosted agent, named by
//     the evals file's agent.name or else the repository root's basename.
//
// The agent markers rank below the plugin ones, so a plugin's own agents/
// folder of subagent definitions (which holds no evals file) never turns a
// plugin repository into an agent one. An agent repository's eval sets carry
// Agent: true and group under the synthetic AgentsGroup plugin.
package layout
