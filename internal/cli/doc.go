// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package cli carries the shared state every evolve subcommand consumes:
// the global flags, the layered .evolve configuration (YAML, JSON, JSONC, or
// TOML — see ConfigExtensions), and the helpers
// that resolve them into a repository, provider set, and token counter. The
// cobra command tree itself lives in cmd/evolve/ (package main) and builds on this
// package.
package cli
