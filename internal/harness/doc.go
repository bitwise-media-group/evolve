// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package harness defines the agent CLIs evolve can drive (Claude Code, OpenAI
// Codex, Gemini, Cursor, Copilot, Antigravity, Grok): their runner-binary candidates,
// credential env vars, skill directories, command construction, and output
// parsing. A harness drives a model — it does not own one — so the model id a
// command targets is supplied as a harness-specific CLI string mapped from the
// canonical model (see internal/model).
//
// Capability gaps are structural: a harness implements the optional EvalRunner
// interface only when its CLI supports a gradable headless run (Gemini does
// not), and engines type-assert and degrade. Cursor, Copilot, and Antigravity
// report no usage (ReportsUsage is false), so their estimate/measured fields
// stay absent end-to-end. Token counting is a vendor concern and lives in
// internal/model, not here.
package harness
