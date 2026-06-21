// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package telemetry

import (
	"io"
	"log/slog"
)

// Mode is the export path Init selected.
type Mode int

const (
	// ModeDisabled keeps telemetry off: no providers, stderr-only logging.
	ModeDisabled Mode = iota
	// ModeFile writes one JSON file per signal under Config.Dir.
	ModeFile
	// ModeEnv defers to autoexport, honoring the OTEL_* environment.
	ModeEnv
)

// String names the mode for diagnostics.
func (m Mode) String() string {
	switch m {
	case ModeFile:
		return "file"
	case ModeEnv:
		return "env"
	default:
		return "disabled"
	}
}

// Config configures Init. The zero value selects disabled mode with a stderr
// logger at the default level.
type Config struct {
	// Dir, when non-empty, selects file mode and names the directory the
	// per-signal JSON files are written under. It comes from --telemetry-dir or
	// the telemetry.dir config key and always wins over OTEL_* env vars.
	Dir string
	// Level gates the stderr text handler; pass the shared --verbose LevelVar so
	// the file and stderr stay in lockstep with the flag.
	Level slog.Leveler
	// ServiceName and ServiceVersion populate the OTEL resource.
	ServiceName    string
	ServiceVersion string
	// Stderr is where the human-readable text handler writes; nil means
	// os.Stderr.
	Stderr io.Writer
}

// otelEnvVars are the standard variables whose presence signals that the user
// wants env-driven export. A value of "none" counts as not requesting export.
var otelEnvVars = []string{
	"OTEL_TRACES_EXPORTER",
	"OTEL_METRICS_EXPORTER",
	"OTEL_LOGS_EXPORTER",
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
}

// resolveMode picks the export mode. A configured directory wins over the
// environment, which wins over disabled; env reads a variable (os.Getenv in
// production, a fake in tests).
func resolveMode(dir string, env func(string) string) Mode {
	if dir != "" {
		return ModeFile
	}
	if otelEnvActive(env) {
		return ModeEnv
	}
	return ModeDisabled
}

// otelEnvActive reports whether any OTEL_* exporter variable requests export.
func otelEnvActive(env func(string) string) bool {
	for _, k := range otelEnvVars {
		if v := env(k); v != "" && v != "none" {
			return true
		}
	}
	return false
}
