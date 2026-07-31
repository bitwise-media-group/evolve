// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package workspace builds the throwaway project directories agent sessions
// run in: every skill of the plugin is symlinked into each requested skills
// dir (so the skill under test has to win against its siblings), case fixture
// files are written at their relative paths, and the result is committed as
// the initial state of a fresh git repository — real projects are repos, and
// both agents and repo-root-detecting tools (actionlint, pre-commit) rely on
// that. The git setup shells out to the git CLI, a deliberate setup-time
// exception to internal/runner being the sole os/exec chokepoint for agent
// execution.
package workspace
