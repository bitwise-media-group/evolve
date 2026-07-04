// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"testing"

	"github.com/spf13/viper"

	"github.com/bitwise-media-group/evolve/internal/report"
)

func TestThresholdsDefaults(t *testing.T) {
	o := &Options{Viper: viper.New(), Root: t.TempDir(), Layout: "auto"}
	if err := o.LoadConfig(newTestCmd()); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	th := o.Thresholds()
	if th.TriggersMinPassRate != report.DefaultTriggersMinPassRate {
		t.Errorf("TriggersMinPassRate = %v, want default %v", th.TriggersMinPassRate, report.DefaultTriggersMinPassRate)
	}
	if th.EvalsMinPassRate != report.DefaultEvalsMinPassRate {
		t.Errorf("EvalsMinPassRate = %v, want default %v", th.EvalsMinPassRate, report.DefaultEvalsMinPassRate)
	}
	if len(th.Models) != 0 {
		t.Errorf("Models = %v, want empty", th.Models)
	}
}

func TestThresholdsConfigOverrides(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".evolve.yaml", `report:
  thresholds:
    triggers_min_pass_rate: 0.9
    models:
      - anthropic/claude-fable-5
`)
	o := &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
	if err := o.LoadConfig(newTestCmd()); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	th := o.Thresholds()
	if th.TriggersMinPassRate != 0.9 {
		t.Errorf("TriggersMinPassRate = %v, want 0.9 (config)", th.TriggersMinPassRate)
	}
	if th.EvalsMinPassRate != report.DefaultEvalsMinPassRate {
		t.Errorf("EvalsMinPassRate = %v, want default %v (unset key)", th.EvalsMinPassRate, report.DefaultEvalsMinPassRate)
	}
	if len(th.Models) != 1 || th.Models[0] != "anthropic/claude-fable-5" {
		t.Errorf("Models = %v, want the configured key", th.Models)
	}
}

// TestThresholdsExplicitZero pins the IsSet override: a configured zero is a
// deliberate "always passes" rate, not a gap to fill with the default.
func TestThresholdsExplicitZero(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".evolve.yaml", `report:
  thresholds:
    evals_min_pass_rate: 0
`)
	o := &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
	if err := o.LoadConfig(newTestCmd()); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if th := o.Thresholds(); th.EvalsMinPassRate != 0 {
		t.Errorf("EvalsMinPassRate = %v, want explicit 0", th.EvalsMinPassRate)
	}
}
