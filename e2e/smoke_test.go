// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package e2e holds end-to-end tests that exercise a real evolve binary
// against the fixture repositories. It is a separate module so the root
// `go test ./...` never picks these tests up; run them via `make smoke`
// or `go -C e2e test`.
package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSmoke runs a real `evolve run all` (live `claude` CLI + credentials)
// against a throwaway copy of the marketplace fixture. The fixture's evals
// are toy specs a live model may legitimately fail, so eval failures are
// tolerated (without --strict they warn and exit 0; an exit 1 is tolerated
// too); the gate is that the pipeline runs end-to-end and produces its
// artifacts.
func TestSmoke(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not found in PATH")
	}

	model := os.Getenv("SMOKE_MODEL")
	if model == "" {
		model = "claude-haiku-4-5"
	}

	bin := buildEvolve(t)

	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.CopyFS(repo, os.DirFS("repos/marketplace")); err != nil {
		t.Fatalf("copy marketplace fixture: %v", err)
	}

	cmd := exec.Command(bin, "run", "all", "--root", repo,
		"--models", model, "--runs", "1", "--jobs", "1", "--timeout", "120")
	cmd.Env = append(os.Environ(), "EVOLVE_CACHE_DIR="+filepath.Join(tmp, "cache"))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	t.Logf("evolve run all output:\n%s", out.String())

	status := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run evolve: %v", err)
		}
		if status = exitErr.ExitCode(); status > 1 {
			t.Fatalf("evolve run all exited %d", status)
		}
	}

	for _, f := range []string{
		"plugins/alpha/evals/alpha-skill/results.json",
		"plugins/beta/evals/beta-skill/results.json",
	} {
		data, err := os.ReadFile(filepath.Join(repo, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if !strings.Contains(string(data), `"executed": true`) {
			t.Errorf("%s is missing executed agent runs", f)
		}
	}

	for _, f := range []string{"EVALUATION.md", "EVALUATION.json"} {
		info, err := os.Stat(filepath.Join(repo, f))
		if err != nil {
			t.Fatalf("stat %s: %v", f, err)
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty — reports were not regenerated", f)
		}
	}

	t.Logf("smoke: ok — pipeline ran end-to-end with %s (evolve exit %d; toy-eval failures tolerated)", model, status)
}

// buildEvolve compiles the root module's binary into a temp dir. The build
// runs from the repository root (one level up), so it uses the root go.mod
// rather than this module's.
func buildEvolve(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "evolve")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/evolve")
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/evolve: %v\n%s", err, out)
	}
	return bin
}
