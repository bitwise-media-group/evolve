// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"slices"
	"strings"
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
	if !slices.Equal(th.Maturity, report.DefaultGatedMaturity) {
		t.Errorf("Maturity = %v, want default %v", th.Maturity, report.DefaultGatedMaturity)
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

// TestParseMaturityFlag pins the --maturity parser: valid tokens resolve, an
// unknown token errors, and an empty set (including the whitespace-only forms
// splitFlag collapses to nothing) is rejected rather than silently gating
// nothing.
func TestParseMaturityFlag(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    []report.Maturity
		wantErr string // substring; "" means no error
	}{
		{"single", "stable", []report.Maturity{report.MaturityStable}, ""},
		{"all three", "stable,unstable,prerelease",
			[]report.Maturity{report.MaturityStable, report.MaturityUnstable, report.MaturityPrerelease}, ""},
		{"trimmed", " unstable , prerelease ",
			[]report.Maturity{report.MaturityUnstable, report.MaturityPrerelease}, ""},
		{"unknown token", "stable,bogus", nil, `unknown maturity level "bogus"`},
		{"unknown level unknown", "unknown", nil, `unknown maturity level "unknown"`},
		{"empty string", "", nil, "empty maturity set"},
		{"commas only", ",,", nil, "empty maturity set"},
		{"whitespace only", "  ", nil, "empty maturity set"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseMaturityFlag(c.in)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestMaturityConfigEmpty pins that an explicitly empty report.thresholds.maturity
// list is rejected the same way an empty --maturity flag is — a configured empty
// set must not silently disarm the gate. A whitespace-only token counts as empty.
func TestMaturityConfigEmpty(t *testing.T) {
	for _, list := range []string{"[]", `[" "]`} {
		dir := t.TempDir()
		writeFile(t, dir, ".evolve.yaml", "report:\n  thresholds:\n    maturity: "+list+"\n")
		o := &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
		if err := o.LoadConfig(newTestCmd()); err != nil {
			t.Fatalf("LoadConfig(%s): %v", list, err)
		}
		if _, err := o.MaturityConfig(); err == nil || !strings.Contains(err.Error(), "empty maturity set") {
			t.Fatalf("MaturityConfig(%s) err = %v, want empty-set rejection", list, err)
		}
	}
}

// TestMaturityConfigTrimsTokens pins that the config path trims token whitespace,
// matching the flag path (splitFlag): a padded config token parses rather than
// hard-erroring as an unknown level.
func TestMaturityConfigTrimsTokens(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".evolve.yaml", "report:\n  thresholds:\n    maturity:\n      - \" stable \"\n      - \"unstable \"\n")
	o := &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
	if err := o.LoadConfig(newTestCmd()); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	levels, err := o.MaturityConfig()
	if err != nil {
		t.Fatalf("MaturityConfig: %v", err)
	}
	if want := []report.Maturity{report.MaturityStable, report.MaturityUnstable}; !slices.Equal(levels, want) {
		t.Errorf("levels = %v, want %v (trimmed)", levels, want)
	}
}

// TestThresholdsMaturityConfig pins how Thresholds() folds the configured
// maturity set in: a valid report.thresholds.maturity flows into
// Thresholds().Maturity, while a malformed token is swallowed (the documented
// fail-safe) so the gate falls back to the default gate-everything set rather
// than an empty one — report --check re-resolves MaturityConfig to surface the
// error to the user; Thresholds() itself must never disarm the gate on a typo.
func TestThresholdsMaturityConfig(t *testing.T) {
	t.Run("valid config flows in", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".evolve.yaml", "report:\n  thresholds:\n    maturity:\n      - stable\n")
		o := &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
		if err := o.LoadConfig(newTestCmd()); err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if got := o.Thresholds().Maturity; !slices.Equal(got, []report.Maturity{report.MaturityStable}) {
			t.Errorf("Maturity = %v, want [stable] from config", got)
		}
	})

	t.Run("malformed token falls back to the default gate-everything set", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".evolve.yaml", "report:\n  thresholds:\n    maturity:\n      - bogus\n")
		o := &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
		if err := o.LoadConfig(newTestCmd()); err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		// Thresholds swallows the parse error and keeps the default set...
		if got := o.Thresholds().Maturity; !slices.Equal(got, report.DefaultGatedMaturity) {
			t.Errorf("Maturity = %v, want the default set (error swallowed, gate not disarmed)", got)
		}
		// ...but MaturityConfig still surfaces it, so report --check can fail loudly.
		if _, err := o.MaturityConfig(); err == nil {
			t.Error("MaturityConfig() = nil error, want the malformed-token error surfaced")
		}
	})
}

// TestResolveMaturity pins the single-owner precedence for report --check's
// gated set: the --maturity flag wins when set, else config, else the default
// gate-everything set — and, unlike Thresholds(), it surfaces a malformed token
// so --check fails loudly rather than silently defaulting.
func TestResolveMaturity(t *testing.T) {
	load := func(t *testing.T, yaml string) *Options {
		t.Helper()
		dir := t.TempDir()
		if yaml != "" {
			writeFile(t, dir, ".evolve.yaml", yaml)
		}
		o := &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
		if err := o.LoadConfig(newTestCmd()); err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		return o
	}

	t.Run("flag wins over config", func(t *testing.T) {
		o := load(t, "report:\n  thresholds:\n    maturity:\n      - unstable\n")
		got, err := o.ResolveMaturity(true, "stable")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, []report.Maturity{report.MaturityStable}) {
			t.Errorf("levels = %v, want [stable] from the flag", got)
		}
	})

	t.Run("config used when flag unset", func(t *testing.T) {
		o := load(t, "report:\n  thresholds:\n    maturity:\n      - prerelease\n")
		got, err := o.ResolveMaturity(false, "stable,unstable,prerelease")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, []report.Maturity{report.MaturityPrerelease}) {
			t.Errorf("levels = %v, want [prerelease] from config", got)
		}
	})

	t.Run("default when neither flag nor config set", func(t *testing.T) {
		o := load(t, "")
		got, err := o.ResolveMaturity(false, "stable,unstable,prerelease")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, report.DefaultGatedMaturity) {
			t.Errorf("levels = %v, want the default set", got)
		}
	})

	t.Run("malformed config surfaces the error", func(t *testing.T) {
		o := load(t, "report:\n  thresholds:\n    maturity:\n      - bogus\n")
		if _, err := o.ResolveMaturity(false, ""); err == nil {
			t.Error("want the malformed-config error surfaced, got nil")
		}
	})

	t.Run("malformed flag surfaces the error", func(t *testing.T) {
		o := load(t, "")
		if _, err := o.ResolveMaturity(true, "bogus"); err == nil {
			t.Error("want the malformed-flag error surfaced, got nil")
		}
	})
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
