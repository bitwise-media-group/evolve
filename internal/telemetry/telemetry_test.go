// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// clearOTELEnv blanks every OTEL_* var so env detection cannot drift a test into
// env mode on a machine that happens to set them.
func clearOTELEnv(t *testing.T) {
	t.Helper()
	for _, k := range otelEnvVars {
		t.Setenv(k, "")
	}
}

func TestInitDisabled(t *testing.T) {
	clearOTELEnv(t)
	prov, shutdown, err := Init(context.Background(), Config{})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if prov.Mode != ModeDisabled {
		t.Errorf("mode = %v, want disabled", prov.Mode)
	}
	if prov.Logger == nil {
		t.Fatal("disabled Init returned a nil logger")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("disabled shutdown: %v", err)
	}
}

func TestInitFileModeCreatesFiles(t *testing.T) {
	clearOTELEnv(t)
	dir := filepath.Join(t.TempDir(), "tel")
	prov, shutdown, err := Init(context.Background(), Config{Dir: dir, ServiceName: "evolve", ServiceVersion: "test"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if prov.Mode != ModeFile {
		t.Errorf("mode = %v, want file", prov.Mode)
	}
	for _, name := range []string{"traces.json", "metrics.json", "logs.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("file-mode shutdown: %v", err)
	}
}

func TestInitFileWinsOverEnv(t *testing.T) {
	clearOTELEnv(t)
	t.Setenv("OTEL_TRACES_EXPORTER", "console")
	dir := filepath.Join(t.TempDir(), "tel")
	prov, shutdown, err := Init(context.Background(), Config{Dir: dir})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer shutdown(context.Background())
	if prov.Mode != ModeFile {
		t.Errorf("mode = %v, want file (the flag wins over OTEL_* env)", prov.Mode)
	}
}
