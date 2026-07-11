// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package report

import (
	"strings"

	goversion "github.com/hashicorp/go-version"
)

// Maturity classifies a plugin's release stability, derived from its
// manifest version.
type Maturity string

const (
	// MaturityStable marks a released version (>= 1.0.0, no prerelease tag).
	MaturityStable Maturity = "stable"
	// MaturityUnstable marks a pre-1.0 version (0.x.y, no prerelease tag).
	MaturityUnstable Maturity = "unstable"
	// MaturityPrerelease marks a version carrying a SemVer prerelease tag
	// (e.g. "2.0.0-rc.1"), regardless of its core version.
	MaturityPrerelease Maturity = "prerelease"
	// MaturityUnknown marks a missing or unparseable version. It is an
	// internal classification, never a user-selectable gate level.
	MaturityUnknown Maturity = "unknown"
)

// stableFloor is the version at and above which a release with no
// prerelease tag is considered stable.
var stableFloor = goversion.Must(goversion.NewVersion("1.0.0"))

// DefaultGatedMaturity is the default gated maturity set for report --check:
// every user-selectable level, so a plugin is gated exactly as it was before
// maturity-aware evidence gating existed (a non-breaking default). It is the
// single source of truth for that default — the CLI runtime default, the
// --maturity flag default, and the configdoc default all derive from it.
// It is a package-level slice, so callers that need their own copy (rather
// than sharing this backing array) must copy it, e.g. via slices.Clone.
var DefaultGatedMaturity = []Maturity{MaturityStable, MaturityUnstable, MaturityPrerelease}

// DefaultGatedMaturityStrings renders DefaultGatedMaturity as its string
// tokens, in order — the shared projection the --maturity flag default and the
// config-reference default both derive from, so neither can drift from
// DefaultGatedMaturity.
func DefaultGatedMaturityStrings() []string {
	levels := make([]string, len(DefaultGatedMaturity))
	for i, m := range DefaultGatedMaturity {
		levels[i] = string(m)
	}
	return levels
}

// DefaultGatedMaturityFlag renders DefaultGatedMaturity as the comma-separated
// string used for the --maturity flag default.
func DefaultGatedMaturityFlag() string {
	return strings.Join(DefaultGatedMaturityStrings(), ",")
}

// Classify returns the Maturity of version, a SemVer string as read from a
// plugin manifest. A version with a prerelease tag classifies as
// MaturityPrerelease regardless of its core version; otherwise a version
// >= 1.0.0 classifies as MaturityStable and a version < 1.0.0 classifies as
// MaturityUnstable. An empty or unparseable version classifies as
// MaturityUnknown.
func Classify(version string) Maturity {
	v, err := goversion.NewVersion(version)
	if err != nil {
		return MaturityUnknown
	}
	if v.Prerelease() != "" {
		return MaturityPrerelease
	}
	if v.LessThan(stableFloor) {
		return MaturityUnstable
	}
	return MaturityStable
}

// ParseMaturity parses s as a user-supplied gate level (from the --maturity
// flag or the report.thresholds.maturity config list), validating it against
// the closed set {stable, unstable, prerelease}. ok is false for any other
// value, including "unknown": MaturityUnknown is an internal classification
// a user can never select as a gate level.
func ParseMaturity(s string) (Maturity, bool) {
	switch Maturity(s) {
	case MaturityStable, MaturityUnstable, MaturityPrerelease:
		return Maturity(s), true
	default:
		return "", false
	}
}
