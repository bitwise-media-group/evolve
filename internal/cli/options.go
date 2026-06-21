// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tailscale/hujson"

	"github.com/bitwise-media-group/evolve/internal/encfmt"
)

// ErrFailures signals that checks or evals ran to completion and at least one
// failed: exit 1, distinct from usage/config errors (exit 2). The run
// subcommands return it only under --strict; by default they warn and exit 0.
// `report --check` returns it unconditionally.
var ErrFailures = errors.New("failures reported")

// ConfigExtensions lists the accepted config-file extensions, in search
// order: the file is .evolve.<ext> at the repository root.
var ConfigExtensions = []string{"yaml", "yml", "json", "jsonc"}

// Options carries the resolved global state every subcommand consumes.
type Options struct {
	Log   *slog.Logger
	Viper *viper.Viper

	Root          string // --root: repository to operate on ("" = walk up from cwd)
	Layout        string // --layout: auto|marketplace|multi|single
	JSON          bool   // --json: machine-readable JSONL progress on stdout
	ResultsFormat string // --results-format: json|jsonc|yaml for results + EVALUATION rollup
	TelemetryDir  string // --telemetry-dir: directory for the OTEL JSON exporter ("" = telemetry disabled)
}

// LoadConfig reads the optional .evolve.<ext> config file from the resolved
// repository root, layering env (EVOLVE_*) and flags above it. Flags that the
// user set explicitly keep precedence because they are bound after the file
// loads.
func (o *Options) LoadConfig(cmd *cobra.Command) error {
	v := o.Viper
	v.SetEnvPrefix("EVOLVE")
	// Dotted keys (telemetry.dir) bind to underscore env vars (EVOLVE_TELEMETRY_DIR);
	// flat keys are unaffected since they hold no dots.
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	dir := o.Root
	if dir == "" {
		dir = "."
	}
	if err := readConfigFile(v, dir); err != nil {
		return err
	}

	if !cmd.Flags().Changed("layout") {
		if l := v.GetString("layout"); l != "" {
			o.Layout = l
		}
	}
	if !cmd.Flags().Changed("results-format") {
		if f := v.GetString("results_format"); f != "" {
			o.ResultsFormat = f
		}
	}
	if !cmd.Flags().Changed("telemetry-dir") {
		if d := v.GetString("telemetry.dir"); d != "" {
			o.TelemetryDir = d
		}
	}
	o.ResultsFormat = encfmt.Canonical(o.ResultsFormat)
	if o.ResultsFormat == "" {
		o.ResultsFormat = "json"
	}
	if !slices.Contains(encfmt.Formats, o.ResultsFormat) {
		return fmt.Errorf("unknown results format %q (want json, jsonc, or yaml)", o.ResultsFormat)
	}
	return nil
}

// ConfigFileName names the loaded config file for provenance output, or ""
// when no config file was found.
func (o *Options) ConfigFileName() string {
	if path := o.Viper.ConfigFileUsed(); path != "" {
		return filepath.Base(path)
	}
	return ""
}

// readConfigFile finds and loads the single .evolve.<ext> in dir. JSONC is
// standardized to plain JSON before it reaches viper, which parses the other
// formats natively. More than one config file is ambiguous and rejected
// rather than silently prioritized.
func readConfigFile(v *viper.Viper, dir string) error {
	var found []string
	for _, ext := range ConfigExtensions {
		path := filepath.Join(dir, ".evolve."+ext)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			found = append(found, path)
		}
	}
	switch len(found) {
	case 0:
		return nil // config is optional
	case 1:
	default:
		return fmt.Errorf("ambiguous config: found %s; keep exactly one", strings.Join(found, ", "))
	}

	path := found[0]
	if strings.HasSuffix(path, ".jsonc") {
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read config %s: %w", path, err)
		}
		std, err := hujson.Standardize(raw)
		if err != nil {
			return fmt.Errorf("parse config %s: %w", path, err)
		}
		v.SetConfigFile(path) // provenance only; the type below wins
		v.SetConfigType("json")
		if err := v.ReadConfig(bytes.NewReader(std)); err != nil {
			return fmt.Errorf("parse config %s: %w", path, err)
		}
		return nil
	}
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	return nil
}
