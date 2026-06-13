// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// newTestCmd mirrors the root command's layout flag, which LoadConfig
// consults for explicit-flag precedence.
func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("layout", "auto", "")
	cmd.Flags().String("results-format", "", "")
	return cmd
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigFormats(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{".evolve.yaml", "layout: marketplace\nchecks:\n  max_skill_lines: 200\n"},
		{".evolve.yml", "layout: marketplace\nchecks:\n  max_skill_lines: 200\n"},
		{".evolve.json", `{"layout": "marketplace", "checks": {"max_skill_lines": 200}}`},
		{".evolve.jsonc", `{
			// comments survive standardization
			"layout": "marketplace",
			"checks": {
				"max_skill_lines": 200, // trailing commas too
			},
		}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, tc.name, tc.content)
			o := &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
			if err := o.LoadConfig(newTestCmd()); err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if o.Layout != "marketplace" {
				t.Errorf("Layout = %q, want marketplace", o.Layout)
			}
			if got := o.Viper.GetInt("checks.max_skill_lines"); got != 200 {
				t.Errorf("checks.max_skill_lines = %d, want 200", got)
			}
			if got := o.ConfigFileName(); got != tc.name {
				t.Errorf("ConfigFileName = %q, want %q", got, tc.name)
			}
		})
	}
}

func TestLoadConfigMissingIsOptional(t *testing.T) {
	o := &Options{Viper: viper.New(), Root: t.TempDir(), Layout: "auto"}
	if err := o.LoadConfig(newTestCmd()); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if o.Layout != "auto" {
		t.Errorf("Layout = %q, want auto", o.Layout)
	}
	if got := o.ConfigFileName(); got != "" {
		t.Errorf("ConfigFileName = %q, want empty", got)
	}
}

func TestLoadConfigAmbiguous(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".evolve.yaml", "layout: multi\n")
	writeFile(t, dir, ".evolve.json", `{"layout": "single"}`)
	o := &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
	err := o.LoadConfig(newTestCmd())
	if err == nil || !strings.Contains(err.Error(), "ambiguous config") {
		t.Fatalf("LoadConfig error = %v, want ambiguous config", err)
	}
}

// TestLoadConfigIgnoresTOML pins the clean break: .evolve.toml is no longer
// a recognized config file.
func TestLoadConfigIgnoresTOML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".evolve.toml", "layout = \"marketplace\"\n")
	o := &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
	if err := o.LoadConfig(newTestCmd()); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if o.Layout != "auto" || o.ConfigFileName() != "" {
		t.Errorf("toml config was loaded: layout=%q file=%q", o.Layout, o.ConfigFileName())
	}
}

func TestLoadConfigResultsFormat(t *testing.T) {
	dir := t.TempDir()
	o := &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
	if err := o.LoadConfig(newTestCmd()); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if o.ResultsFormat != "json" {
		t.Errorf("default ResultsFormat = %q, want json", o.ResultsFormat)
	}

	writeFile(t, dir, ".evolve.yaml", "results_format: yml\n")
	o = &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
	if err := o.LoadConfig(newTestCmd()); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if o.ResultsFormat != "yaml" {
		t.Errorf("ResultsFormat = %q, want yaml (yml canonicalized)", o.ResultsFormat)
	}

	// An explicit flag beats the config file.
	cmd := newTestCmd()
	if err := cmd.Flags().Set("results-format", "jsonc"); err != nil {
		t.Fatal(err)
	}
	o = &Options{Viper: viper.New(), Root: dir, Layout: "auto", ResultsFormat: "jsonc"}
	if err := o.LoadConfig(cmd); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if o.ResultsFormat != "jsonc" {
		t.Errorf("ResultsFormat = %q, want jsonc (explicit flag)", o.ResultsFormat)
	}

	o = &Options{Viper: viper.New(), Root: t.TempDir(), Layout: "auto", ResultsFormat: "toml"}
	if err := o.LoadConfig(newTestCmd()); err == nil {
		t.Error("LoadConfig: want error for unknown results format")
	}
}

func TestLoadConfigExplicitFlagWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".evolve.yaml", "layout: marketplace\n")
	cmd := newTestCmd()
	if err := cmd.Flags().Set("layout", "single"); err != nil {
		t.Fatal(err)
	}
	o := &Options{Viper: viper.New(), Root: dir, Layout: "single"}
	if err := o.LoadConfig(cmd); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if o.Layout != "single" {
		t.Errorf("Layout = %q, want single (explicit flag)", o.Layout)
	}
}

func TestLoadConfigInvalidJSONC(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".evolve.jsonc", "{ not valid")
	o := &Options{Viper: viper.New(), Root: dir, Layout: "auto"}
	if err := o.LoadConfig(newTestCmd()); err == nil {
		t.Fatal("LoadConfig: want error for invalid jsonc")
	}
}
