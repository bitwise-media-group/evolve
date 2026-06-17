// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package provider defines the agent providers the harness can drive
// (Anthropic, OpenAI, Google, Cursor, Copilot): their model matrices with
// pricing, runner-CLI command construction, output parsing, and token-counting
// clients.
//
// Capability gaps are structural: providers implement the optional EvalRunner
// and TokenCounter interfaces only when the underlying platform supports
// them, and engines type-assert and degrade. Cursor and Copilot implement
// neither token counting nor usage reporting, so their estimate/measured
// fields stay absent end-to-end.
package provider
