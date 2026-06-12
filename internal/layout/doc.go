// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package layout detects which of the three supported repository shapes a
// root directory has and enumerates its plugins and eval definitions.
//
// The shapes, in detection precedence order:
//
//   - marketplace: .claude-plugin/marketplace.json at the root, plugins under
//     plugins/<name>/.
//   - multi: plugins/<name>/.claude-plugin/plugin.json present but no
//     marketplace manifest; marketplace checks are skipped.
//   - single: the repository root IS the plugin (.claude-plugin/plugin.json at
//     the root) with skills/ and evals/ beside it.
package layout
