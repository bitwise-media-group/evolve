// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package model defines the model vendors (Anthropic, OpenAI, Google, Cursor,
// xAI) and their canonical models: provider-qualified ids, pricing, the harnesses
// each model can be driven by, and the vendor token-counting clients.
//
// It is the lowest-level domain package — it imports no other internal package
// — and owns the value types the runner and engines share (CommandSpec,
// EvalInput, Usage) plus the pricing helpers. A model's identity
// (Model.Key/BareID) is independent of the harness that runs it: the same model
// keeps one results key whether Claude Code or Copilot executes it, and the
// Supported map records the harness-specific CLI id each driver needs.
//
// Token counting lives here, keyed by provider id (CounterFor), because it is a
// vendor API: Cursor has no counter, so its models stay token-less end-to-end.
package model
