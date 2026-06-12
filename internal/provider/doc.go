// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package provider defines the agent providers the harness can drive
// (Anthropic, OpenAI, Google, Cursor): their model matrices with pricing,
// runner-CLI command construction, output parsing, and token-counting
// clients.
//
// Capability gaps are structural: providers implement the optional CaseRunner
// and TokenCounter interfaces only when the underlying platform supports
// them, and engines type-assert and degrade. Cursor implements neither
// token counting nor usage reporting, so its estimate/measured fields stay
// absent end-to-end.
package provider
