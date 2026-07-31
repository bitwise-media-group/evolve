// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/runner"
)

// listingHarness is a fake harness with the OfferedModels capability.
type listingHarness struct {
	harness.Harness
	offered []string
	err     error
}

func (l listingHarness) ListOfferedModels(context.Context, harness.ProbeExec) ([]string, error) {
	return l.offered, l.err
}

// onPathHarness returns a harness whose first CLI candidate resolves on PATH:
// a stub executable is created in a temp dir prepended to PATH.
func onPathBase(t *testing.T) harness.Harness {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "claude")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return harness.NewClaude()
}

func TestProbeOfferedModels(t *testing.T) {
	base := onPathBase(t)
	hs := []harness.Harness{
		listingHarness{Harness: base, offered: []string{"Sonnet 5"}},
		harness.NewGemini(), // no OfferedModels capability
	}
	got := ProbeOfferedModels(t.Context(), &runner.Exec{}, hs, time.Second)
	if !slices.Equal(got[base.ID()], []string{"Sonnet 5"}) {
		t.Errorf("offered[%s] = %v, want [Sonnet 5]", base.ID(), got[base.ID()])
	}
	if _, ok := got[harness.NewGemini().ID()]; ok {
		t.Error("harness without the capability should be absent (unknown)")
	}
}

func TestProbeOfferedModelsFailuresAbsent(t *testing.T) {
	base := onPathBase(t)
	hs := []harness.Harness{
		listingHarness{Harness: base, err: errors.New("boom")},
	}
	if got := ProbeOfferedModels(t.Context(), &runner.Exec{}, hs, time.Second); len(got) != 0 {
		t.Errorf("failed probe = %v, want empty (unknown fails open)", got)
	}
}

// fakeProbeRunner records the spec and serves canned line-oriented stdout.
type fakeProbeRunner struct {
	spec  model.CommandSpec
	lines []string
}

func (f *fakeProbeRunner) Run(_ context.Context, spec model.CommandSpec, _ time.Duration,
	scan *runner.Scan) (runner.Result, error) {
	f.spec = spec
	if scan == nil {
		return runner.Result{Stdout: []byte(joinLines(f.lines))}, nil
	}
	for _, line := range f.lines {
		if scan.OnLine([]byte(line + "\n")) {
			return runner.Result{Hit: true}, nil
		}
	}
	return runner.Result{}, nil
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

func TestProbeExecResolvesCLIAndStopsEarly(t *testing.T) {
	h := onPathBase(t)
	fake := &fakeProbeRunner{lines: []string{"skip", "stop", "never-read"}}
	exec := probeExec(fake, h, time.Second)

	out, err := exec(t.Context(), model.CommandSpec{Argv: []string{"claude", "app-server"}},
		func(line []byte) bool { return string(line) == "stop\n" })
	if err != nil {
		t.Fatalf("probeExec: %v", err)
	}
	if got := string(out); got != "skip\nstop\n" {
		t.Errorf("collected = %q, want lines up to and including the stop line", got)
	}
	if filepath.Base(fake.spec.Argv[0]) != "claude" || fake.spec.Argv[0] == "claude" {
		t.Errorf("Argv[0] = %q, want resolved absolute CLI path", fake.spec.Argv[0])
	}
}

func TestProbeExecUnfinishedProtocolErrors(t *testing.T) {
	h := onPathBase(t)
	fake := &fakeProbeRunner{lines: []string{"noise"}}
	exec := probeExec(fake, h, time.Second)
	if _, err := exec(t.Context(), model.CommandSpec{Argv: []string{"claude"}},
		func([]byte) bool { return false }); err == nil {
		t.Error("probe that never saw its response should error, not return partial output")
	}
}
