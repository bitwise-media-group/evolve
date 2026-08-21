// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package grade evaluates case assertions against an agent's workspace and
// final output: deterministic checks (files, regexes, commands) first, then
// one LLM-judge session grading all of the case's subjective (llm) assertions
// at once — the judge runs with the full eval tool posture, so evidence is
// collected before it can touch the workspace. The judge is one pinned model
// (DefaultJudgeModel unless judge_model overrides it) driven by whichever
// installed harness supports it, so grading stays comparable across the
// providers under test; the package itself is harness-free — it hands the
// batch prompt to an injected Judge and parses the per-assertion verdicts it
// returns.
package grade
