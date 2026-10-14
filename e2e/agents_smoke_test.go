// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentsSmoke drives a real evolve binary against a live AG-UI agent:
// checks, evals, and report over a throwaway single-agent repository whose
// local endpoint is EVOLVE_AGUI_SMOKE_URL. Any AG-UI 1.0 agent will do — for
// a deterministic one that needs no LLM key, serve AG-UI's
// server-starter-all-features Python example and point the variable at one
// of its routes:
//
//	EVOLVE_AGUI_SMOKE_URL=http://localhost:8000/agentic_chat go -C e2e test -run TestAgentsSmoke
//
// The suite's assertions are deterministic (no judge CLI needed): the
// conversation must complete and conform to the protocol. Frontend tool calls
// are answered generically, so routes that ask for human input work too.
func TestAgentsSmoke(t *testing.T) {
	url := os.Getenv("EVOLVE_AGUI_SMOKE_URL")
	if url == "" {
		t.Skip("EVOLVE_AGUI_SMOKE_URL not set (point it at a running AG-UI agent)")
	}
	bin := buildEvolve(t)

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	suite := map[string]any{
		"agent": map[string]any{"name": "smoke", "endpoints": map[string]any{"local": map[string]any{"url": url}}},
		"evals": []any{map[string]any{
			"id":        "hello",
			"prompt":    "Hello! Please say hi back.",
			"responses": []any{map[string]any{"tool": ".", "result": "ok", "repeat": true}},
			"max_turns": 5,
			"assertions": []any{
				map[string]any{"type": "conformance"},
				map[string]any{"type": "outcome", "expect": "completed"},
			},
		}},
	}
	data, err := json.MarshalIndent(suite, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "agent", "evals.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, append(args, "--root", repo)...)
		cmd.Env = append(os.Environ(), "EVOLVE_NO_TUI=1", "EVOLVE_CACHE_DIR="+filepath.Join(repo, ".cache"))
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		t.Logf("evolve %s:\n%s", strings.Join(args, " "), out.String())
		if err != nil {
			t.Fatalf("evolve %s: %v", strings.Join(args, " "), err)
		}
		return out.String()
	}
	run("run", "checks", "--strict")
	run("run", "evals", "--endpoint", "local", "--strict")
	run("report")

	results, err := os.ReadFile(filepath.Join(repo, "agent", "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"ag-ui/local"`, `"executed": true`, `"conversation"`} {
		if !strings.Contains(string(results), want) {
			t.Errorf("results.json missing %s:\n%s", want, results)
		}
	}
	if strings.Contains(string(results), `"runtime_error"`) {
		t.Errorf("the conversation errored:\n%s", results)
	}
	report, err := os.ReadFile(filepath.Join(repo, "EVALUATION.md"))
	if err != nil || !strings.Contains(string(report), "# Agent evaluations") {
		t.Errorf("EVALUATION.md = %q, %v", report, err)
	}
}
