// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package grade evaluates case assertions against an agent's workspace and
// final output: deterministic checks (files, regexes, commands) plus an
// LLM judge for subjective assertions. The judge always runs through the
// claude CLI regardless of the model under test, so grading stays comparable
// across providers.
package grade
