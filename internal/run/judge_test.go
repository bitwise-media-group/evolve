// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/runner"
)

// specRunner records the spec it ran and returns a canned result.
type specRunner struct {
	gotSpec    model.CommandSpec
	gotTimeout time.Duration
	result     runner.Result
	err        error
}

func (s *specRunner) Run(_ context.Context, spec model.CommandSpec, timeout time.Duration, _ *runner.Scan) (runner.Result, error) {
	s.gotSpec, s.gotTimeout = spec, timeout
	return s.result, s.err
}

// fakeJudgeSelection binds the fake harness (CLI "sh", so it resolves on PATH)
// to its canonical model.
func fakeJudgeSelection() harness.Selection {
	p := &fakeEvalProvider{}
	return harness.Selection{Model: p.canonicalModel(), Harness: p}
}

func TestHarnessJudge(t *testing.T) {
	r := &specRunner{result: runner.Result{Stdout: []byte(`{"verdicts": [{"id": 1, "passed": true}]}`)}}
	j, err := NewHarnessJudge(fakeJudgeSelection(), r, true)
	if err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()
	text, err := j.Judge(context.Background(), ws, "the prompt", 7*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `"passed": true`) {
		t.Errorf("text = %q", text)
	}
	// The spec is the harness's EvalSpec at the judge turn ceiling with Argv[0]
	// resolved to the installed CLI — the fake harness's CLI is "sh", which
	// LookPath resolves absolutely.
	if !strings.HasSuffix(r.gotSpec.Argv[0], "/sh") {
		t.Errorf("Argv[0] = %q, want resolved sh path", r.gotSpec.Argv[0])
	}
	if r.gotSpec.Argv[1] != "AGENT" || r.gotSpec.Argv[2] != "the prompt" {
		t.Errorf("argv = %v, want the fake EvalSpec shape", r.gotSpec.Argv)
	}
	if r.gotSpec.Argv[3] != strconv.Itoa(model.DefaultJudgeMaxTurns) {
		t.Errorf("MaxTurns = %s, want the judge turn ceiling %d", r.gotSpec.Argv[3], model.DefaultJudgeMaxTurns)
	}
	if r.gotSpec.Dir != ws {
		t.Errorf("Dir = %q, want the eval workspace", r.gotSpec.Dir)
	}
	if r.gotTimeout != 7*time.Second {
		t.Errorf("timeout = %s, want 7s", r.gotTimeout)
	}
}

// TestHarnessJudgeClaudePosture pins the real claude judge argv: the eval
// posture (permissions bypassed, no tool allowlist — evolve's sandbox is the
// confinement) at the raised judge turn ceiling. Built directly rather than
// through NewHarnessJudge so the test never needs a claude CLI on PATH.
func TestHarnessJudgeClaudePosture(t *testing.T) {
	// A set credential var keeps the harness isolation setup from consulting
	// the host's real Keychain.
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	c := harness.NewClaude()
	m := model.Model{
		ID: "anthropic/claude-sonnet-5", ProviderID: "anthropic",
		Supported: map[string]string{"claude": "sonnet"}, Preferred: "claude",
	}
	r := &specRunner{result: runner.Result{Stdout: []byte(`{"verdicts": [{"id": 1, "passed": true}]}`)}}
	j := &HarnessJudge{
		sel:  harness.Selection{Model: m, Harness: c},
		eval: c, cli: "claude", runner: r, hostSandboxed: true,
	}
	if _, err := j.Judge(context.Background(), t.TempDir(), "verdict?", time.Second); err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(r.gotSpec.Argv, " ")
	if !strings.Contains(argv, "--permission-mode bypassPermissions") {
		t.Errorf("want --permission-mode bypassPermissions: %v", r.gotSpec.Argv)
	}
	if !strings.Contains(argv, "--max-turns 16") {
		t.Errorf("want --max-turns 16: %v", r.gotSpec.Argv)
	}
	if strings.Contains(argv, "--allowedTools") {
		t.Errorf("want no --allowedTools on the judge: %v", r.gotSpec.Argv)
	}
}

func TestHarnessJudgeErrors(t *testing.T) {
	tests := []struct {
		name   string
		runner *specRunner
		want   string
	}{
		{"run error", &specRunner{err: errors.New("exec blew up")}, "exec blew up"},
		{"timeout", &specRunner{result: runner.Result{TimedOut: true}}, "timed out"},
		// Empty stdout trips the fake harness's RuntimeError, mapping an unusable
		// judge session to an error instead of grading garbage.
		{"runtime error", &specRunner{result: runner.Result{ExitCode: 1}}, "empty CLI output"},
	}
	for _, tt := range tests {
		j, err := NewHarnessJudge(fakeJudgeSelection(), tt.runner, false)
		if err != nil {
			t.Fatal(err)
		}
		_, err = j.Judge(context.Background(), t.TempDir(), "p", time.Second)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestNewHarnessJudgeRejectsNonEvalHarness(t *testing.T) {
	sel := harness.Selection{Model: model.Model{ID: "google/gemini"}, Harness: harness.NewGemini()}
	if _, err := NewHarnessJudge(sel, &specRunner{}, false); err == nil ||
		!strings.Contains(err.Error(), "cannot run headless judge sessions") {
		t.Errorf("err = %v, want headless-judge rejection", err)
	}
}

func TestUnavailableJudge(t *testing.T) {
	_, err := UnavailableJudge{Reason: "no harness installed"}.Judge(context.Background(), "", "", 0)
	if err == nil || !strings.Contains(err.Error(), "judge unavailable: no harness installed") {
		t.Errorf("err = %v, want the resolution reason", err)
	}
}
