// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package run implements the three eval engines behind `evolve run`: static
// checks (Tier 0), trigger accuracy (Tier 1), and behavioral evals (Tier 2).
//
// Checks port the Python harness's run_checks.py — SKILL.md frontmatter,
// plugin manifest structure and version sync, and marketplace consistency —
// driven by the detected repository layout instead of flags: marketplace
// repos get marketplace + plugin + skill checks, multi-plugin repos skip the
// marketplace layer, and single-plugin repos are checked as one plugin rooted
// at the repository root (whose manifests must agree with each other rather
// than a directory name).
//
// Triggers: for every evals/<skill>/triggers.json, each query runs through a
// headless agent session in a throwaway workspace where the plugin's skills
// are installed, and the engine checks whether the skill under test
// activates. A query passes when its observed trigger rate across N runs
// agrees with should_trigger (threshold 0.5). Each selected model also gets a
// token estimate per query — SKILL.md plus the query, the marginal context a
// triggering eval loads — priced at the model's input rate.
//
// Evals: for every evals/<skill>/evals.json, each eval's prompt runs through
// a headless agent session in a throwaway fixture workspace (with the
// plugin's skills installed), then assertions are graded — deterministic
// first, LLM judge last. Executed runs record the harness-reported usage of
// the live session where the provider exposes it; the agent's wall clock
// excludes grading so the model-speed signal stays clean.
package run
