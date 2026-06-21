// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package telemetry

import "testing"

func TestResolveMode(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		env  map[string]string
		want Mode
	}{
		{"disabled when nothing set", "", nil, ModeDisabled},
		{"file when dir set", "/tmp/telemetry", nil, ModeFile},
		{"file wins over env", "/tmp/telemetry", map[string]string{"OTEL_TRACES_EXPORTER": "otlp"}, ModeFile},
		{"env from traces exporter", "", map[string]string{"OTEL_TRACES_EXPORTER": "console"}, ModeEnv},
		{"env from otlp endpoint", "", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318"}, ModeEnv},
		{"env from logs endpoint", "", map[string]string{"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": "http://localhost:4318"}, ModeEnv},
		{"none keeps disabled", "", map[string]string{"OTEL_TRACES_EXPORTER": "none"}, ModeDisabled},
		{"empty keeps disabled", "", map[string]string{"OTEL_TRACES_EXPORTER": ""}, ModeDisabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := func(k string) string { return tt.env[k] }
			if got := resolveMode(tt.dir, env); got != tt.want {
				t.Errorf("resolveMode(%q, env) = %v, want %v", tt.dir, got, tt.want)
			}
		})
	}
}

func TestModeString(t *testing.T) {
	for mode, want := range map[Mode]string{
		ModeDisabled: "disabled",
		ModeFile:     "file",
		ModeEnv:      "env",
	} {
		if got := mode.String(); got != want {
			t.Errorf("Mode(%d).String() = %q, want %q", mode, got, want)
		}
	}
}
