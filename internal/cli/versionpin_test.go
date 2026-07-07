// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func pinOptions(pin string) *Options {
	v := viper.New()
	if pin != "" {
		v.Set("version", pin)
	}
	return &Options{Viper: v}
}

func TestVersionPin(t *testing.T) {
	if got := pinOptions("").VersionPin(); got != "" {
		t.Errorf("unpinned VersionPin() = %q, want empty", got)
	}
	if got := pinOptions("  ~> 0.4  ").VersionPin(); got != "~> 0.4" {
		t.Errorf("VersionPin() = %q, want trimmed %q", got, "~> 0.4")
	}
}

func TestCheckVersionPin(t *testing.T) {
	tests := []struct {
		name    string
		pin     string
		binary  string
		wantErr bool
	}{
		{"no pin passes any binary", "", "dev", false},
		{"exact pin satisfied", "0.4.0", "0.4.0", false},
		{"exact pin violated", "0.4.0", "0.5.0", true},
		{"pessimistic pin satisfied", "~> 0.4", "0.4.9", false},
		{"pessimistic pin violated above", "~> 0.4", "1.0.0", true},
		{"range lower bound inclusive", ">= 0.4, < 1", "0.4.0", false},
		{"range upper bound exclusive", ">= 0.4, < 1", "1.0.0", true},
		{"range below lower bound", ">= 0.4, < 1", "0.3.9", true},
		{"v prefix tolerated", ">= 0.4, < 1", "v0.4.2", false},
		{"invalid constraint is a config error", "banana", "0.4.0", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var warn bytes.Buffer
			err := pinOptions(tc.pin).CheckVersionPin(tc.binary, &warn)
			if (err != nil) != tc.wantErr {
				t.Errorf("CheckVersionPin(%q) against pin %q: err = %v, wantErr %v",
					tc.binary, tc.pin, err, tc.wantErr)
			}
			if warn.Len() != 0 {
				t.Errorf("unexpected warning: %s", warn.String())
			}
		})
	}
}

// TestCheckVersionPinNonReleaseBuild pins the non-release escape hatch: a
// binary version that is not semver (the from-source "dev" default) or carries
// a prerelease (git-describe, goreleaser snapshots) bypasses the pin with a
// warning rather than blocking contributors, matching terraform's behavior —
// go-version constraints would otherwise reject every prerelease outright.
func TestCheckVersionPinNonReleaseBuild(t *testing.T) {
	for _, binary := range []string{"dev", "0.5.0-snapshot-abc", "v0.5.1-9-ge9ffd14-dirty"} {
		t.Run(binary, func(t *testing.T) {
			var warn bytes.Buffer
			if err := pinOptions(">= 99").CheckVersionPin(binary, &warn); err != nil {
				t.Fatalf("non-release build must skip the pin, got %v", err)
			}
			if !strings.Contains(warn.String(), `version pin ">= 99"`) {
				t.Errorf("warning = %q, want it to name the skipped pin", warn.String())
			}
		})
	}
}
