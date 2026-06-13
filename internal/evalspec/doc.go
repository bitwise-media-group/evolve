// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package evalspec parses the authored eval definitions: triggers and evals
// files in skill-creator's envelope shape ({skill_name?, evals|triggers:
// [...]}). The eval object is a superset of skill-creator's — id (string or
// integer), prompt, expected_output, files (fixture paths), expectations —
// plus evolve's extensions: deterministic assertions, max_turns,
// timeout_seconds, allowed_tools, and skip_providers. Loading normalizes the
// superset: integer ids become strings, expectations expand to llm
// assertions graded before the authored ones, and fixture paths resolve
// against the file's directory.
package evalspec
