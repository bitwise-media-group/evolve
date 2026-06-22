// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package plan is the single owner of what a sweep runs, for which models, and in
// what order. It models the run as an ordered tree — plugin → skill → model → unit
// (triggers before evals) → case — and resolves a user Selection into that tree,
// marking each case queued (it will run this session) or prior (shown read-only
// from the last committed result).
//
// The engine (internal/run) executes the plan, the selection form previews it
// live, and the live dashboard renders it. plan imports neither run (execution)
// nor tui (display), so those consumers cannot drift from it: there is one
// definition of ordering and of "what runs", and everyone reads it.
package plan
