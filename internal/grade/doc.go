// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package grade evaluates case assertions against an agent's workspace and
// final output: deterministic checks (files, regexes, commands) plus an
// LLM judge for subjective assertions. The judge is one pinned model
// (DefaultJudgeModel unless judge_model overrides it) driven by whichever
// installed harness supports it, so grading stays comparable across the
// providers under test; the package itself is harness-free — it hands the
// judge prompt to an injected Judge and parses the verdict it returns.
package grade
