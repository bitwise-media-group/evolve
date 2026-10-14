// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestDoctorAgentEndpoints pins the doctor's agent table: every agent's
// endpoints with the URL host as authored and its ${VAR}s marked set or
// unset — never a value, and no live call.
func TestDoctorAgentEndpoints(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agents", "support", "evals.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"agent":{"endpoints":{
		"local":{"url":"http://localhost:8000/"},
		"staging":{"url":"https://${DOCTOR_HOST}/agents/support","headers":{"Authorization":"Bearer ${DOCTOR_TOKEN}"}}}},
		"evals":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	saved, savedRoot := opts.Viper, opts.Root
	t.Cleanup(func() { opts.Viper, opts.Root = saved, savedRoot })
	opts.Viper, opts.Root = viper.New(), root
	t.Setenv("DOCTOR_TOKEN", "s3cret")

	var out bytes.Buffer
	if err := renderAgentEndpoints(&out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"AGENT", "support  local", "localhost:8000", "none",
		"support  staging", "${DOCTOR_HOST}", "DOCTOR_TOKEN (set), DOCTOR_HOST (UNSET)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "s3cret") {
		t.Errorf("doctor output leaks a secret:\n%s", got)
	}

	// Outside an agent repository the table is absent.
	opts.Root = filepath.Join("..", "..", "e2e", "repos", "single")
	out.Reset()
	if err := renderAgentEndpoints(&out); err != nil || out.Len() != 0 {
		t.Errorf("plugin repo: output %q, err %v; want nothing", out.String(), err)
	}
}
