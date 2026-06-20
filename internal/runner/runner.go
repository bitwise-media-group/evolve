// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package runner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bitwise-media-group/evolve/internal/provider"
)

const (
	stderrTailBytes = 4096
	maxStdoutBytes  = 32 << 20 // collect mode cap; the stream keeps draining past it
	waitDelay       = 5 * time.Second
)

// Result is the outcome of one agent run.
type Result struct {
	Hit        bool          // scan mode: onLine reported a hit
	Stdout     []byte        // collect mode: full stdout (bounded)
	TimedOut   bool          // the per-run timeout expired
	ExitCode   int           // process exit code (-1 when killed)
	StderrTail string        // last bytes of stderr, for timeout diagnostics
	Elapsed    time.Duration // wall clock of the agent run
}

// Exec runs commands for real.
type Exec struct {
	// Sandbox, when enabled, confines each run's filesystem writes by wrapping
	// the command in an OS sandbox. The zero value is disabled, so callers and
	// tests that build Exec{} run unconfined as before.
	Sandbox Sandbox
}

// Run executes spec with the given timeout. When onLine is non-nil, stdout is
// scanned line by line and onLine returning true ends the run early with
// Hit=true; otherwise stdout is collected into Result.Stdout. A timed-out run
// is not an error: it returns TimedOut=true with whatever output arrived, so
// trigger runs count as no-trigger and case runs grade partial output. The
// returned error is non-nil only for unstartable commands or parent-context
// cancellation (Ctrl-C).
func (e *Exec) Run(ctx context.Context, spec provider.CommandSpec, timeout time.Duration,
	onLine func([]byte) bool) (Result, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	argv, err := e.Sandbox.wrap(spec.Dir, spec.Argv)
	if err != nil {
		return Result{}, err
	}

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	configureProcessTreeKill(cmd)
	cmd.WaitDelay = waitDelay

	stderr := &ring{max: stderrTailBytes}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}

	var collected bytes.Buffer
	hit := false
	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			switch {
			case hit:
				// Already hit: keep draining to EOF so Wait can return.
			case onLine != nil:
				if onLine(line) {
					hit = true
					cancel() // early exit; the group kill ends the stream
				}
			case collected.Len() < maxStdoutBytes:
				collected.Write(line)
			}
		}
		if err != nil {
			break // EOF or pipe closed by the kill
		}
	}
	waitErr := cmd.Wait()

	res := Result{
		Hit:        hit,
		Stdout:     collected.Bytes(),
		StderrTail: stderr.String(),
		Elapsed:    time.Since(start),
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	switch {
	case ctx.Err() != nil:
		return res, ctx.Err() // interrupted from above; abort the sweep
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		res.TimedOut = true
		return res, nil
	default:
		// Runner exit codes are noise (headless CLIs exit non-zero on
		// max-turns, partial runs, ...); the output already tells the story.
		_ = waitErr
		return res, nil
	}
}

// ring keeps the last max bytes written, for stderr tails.
type ring struct {
	buf []byte
	max int
}

func (r *ring) Write(p []byte) (int, error) {
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.max {
		r.buf = r.buf[len(r.buf)-r.max:]
	}
	return len(p), nil
}

func (r *ring) String() string {
	return strings.TrimSpace(string(r.buf))
}
