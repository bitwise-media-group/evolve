// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package report

import (
	"slices"
	"testing"
)

// TestDefaultGatedMaturityProjections pins the "single source of truth" contract:
// the string projection and the comma-joined flag default both derive from
// DefaultGatedMaturity and must stay in lockstep with it — the --maturity flag
// default and the config-reference default read from these, so a drift here would
// silently change the gate's default set.
func TestDefaultGatedMaturityProjections(t *testing.T) {
	wantStrings := []string{"stable", "unstable", "prerelease"}
	if got := DefaultGatedMaturityStrings(); !slices.Equal(got, wantStrings) {
		t.Errorf("DefaultGatedMaturityStrings() = %v, want %v", got, wantStrings)
	}
	if got, want := DefaultGatedMaturityFlag(), "stable,unstable,prerelease"; got != want {
		t.Errorf("DefaultGatedMaturityFlag() = %q, want %q", got, want)
	}
	// The projection must track DefaultGatedMaturity itself, not a hardcoded copy.
	derived := make([]string, len(DefaultGatedMaturity))
	for i, m := range DefaultGatedMaturity {
		derived[i] = string(m)
	}
	if got := DefaultGatedMaturityStrings(); !slices.Equal(got, derived) {
		t.Errorf("DefaultGatedMaturityStrings() = %v, drifted from DefaultGatedMaturity %v", got, derived)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		version string
		want    Maturity
	}{
		// The <1.0.0 vs >=1.0.0 boundary (AC7/AC8).
		{"0.9.9", MaturityUnstable},
		{"1.0.0", MaturityStable},
		{"1.2.3", MaturityStable},
		{"0.0.1", MaturityUnstable},
		{"10.4.2", MaturityStable},
		// A prerelease tag wins regardless of the core version.
		{"1.0.0-alpha.1", MaturityPrerelease},
		{"2.0.0-rc.1", MaturityPrerelease},
		{"0.9.0-beta", MaturityPrerelease},
		// Missing or unparseable versions are never gated (AC9).
		{"", MaturityUnknown},
		{"not-a-version", MaturityUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := Classify(tt.version); got != tt.want {
				t.Errorf("Classify(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

func TestParseMaturity(t *testing.T) {
	tests := []struct {
		in     string
		want   Maturity
		wantOK bool
	}{
		{"stable", MaturityStable, true},
		{"unstable", MaturityUnstable, true},
		{"prerelease", MaturityPrerelease, true},
		// "unknown" is an internal classification, never a selectable gate level.
		{"unknown", "", false},
		// The closed set is case-sensitive and rejects anything else.
		{"STABLE", "", false},
		{"", "", false},
		{"garbage", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := ParseMaturity(tt.in)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("ParseMaturity(%q) = (%q, %t), want (%q, %t)", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
