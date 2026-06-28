// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"slices"
	"testing"

	"github.com/spf13/viper"
)

// TestChecksConfigDefaults pins the baseline when no checks.* keys are set:
// ChecksConfig returns DefaultCheckConfig untouched.
func TestChecksConfigDefaults(t *testing.T) {
	o := &Options{Viper: viper.New()}
	cfg := o.ChecksConfig()

	if !slices.Equal(cfg.PluginManifests, []string{"claude", "codex"}) {
		t.Errorf("PluginManifests = %v, want [claude codex]", cfg.PluginManifests)
	}
	if !cfg.Marketplace {
		t.Error("Marketplace = false, want true (default)")
	}
	if !cfg.Signals.Enabled {
		t.Error("Signals.Enabled = false, want true (default)")
	}
}

// TestChecksConfigOverrides covers the plugin_manifests, marketplace, and
// signals override branches: each config key replaces the corresponding
// default.
func TestChecksConfigOverrides(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".evolve.yaml", "checks:\n  plugin_manifests: [claude]\n  marketplace: false\n  signals: false\n")

	o := &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
	if err := o.LoadConfig(newTestCmd()); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg := o.ChecksConfig()

	if !slices.Equal(cfg.PluginManifests, []string{"claude"}) {
		t.Errorf("PluginManifests = %v, want [claude]", cfg.PluginManifests)
	}
	if cfg.Marketplace {
		t.Error("Marketplace = true, want false (overridden)")
	}
	if cfg.Signals.Enabled {
		t.Error("Signals.Enabled = true, want false (overridden)")
	}
}
