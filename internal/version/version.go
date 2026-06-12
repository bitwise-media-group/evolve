// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package version exposes build metadata stamped in via -ldflags.
package version

// Injected at build time via -ldflags (see Makefile and .goreleaser.yaml).
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)
