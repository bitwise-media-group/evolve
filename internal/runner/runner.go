// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package runner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/bitwise-media-group/evolve/internal/provider"
)

// scopeName is this package's OpenTelemetry instrumentation scope.
const scopeName = "github.com/bitwise-media-group/evolve/internal/runner"

// obs lazily builds the tracer and instruments on first Run, after telemetry
// has installed the global providers; before then otel's globals are no-ops, so
// instrumentation is harmless when telemetry is disabled.
var obs = sync.OnceValue(newObservability)

// observability holds the agent-exec span tracer and its metrics.
type observability struct {
	tracer       trace.Tracer
	execDuration metric.Float64Histogram
	timeouts     metric.Int64Counter
}

func newObservability() *observability {
	m := otel.Meter(scopeName)
	dur, _ := m.Float64Histogram("evolve.agent.exec.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Wall-clock duration of one agent CLI invocation."))
	to, _ := m.Int64Counter("evolve.agent.exec.timeout",
		metric.WithUnit("{exec}"),
		metric.WithDescription("Agent CLI invocations that hit the per-run timeout."))
	return &observability{tracer: otel.Tracer(scopeName), execDuration: dur, timeouts: to}
}

// observe records the exec's span attributes, metrics, and a finish log, and
// marks the span errored only on runErr (a started-but-cancelled or unstartable
// run). A timeout or non-zero exit is a normal outcome here, not a span error.
func (o *observability) observe(ctx context.Context, span trace.Span, spec provider.CommandSpec,
	res Result, runErr error) {
	span.SetAttributes(
		attribute.Int("exit_code", res.ExitCode),
		attribute.Bool("timed_out", res.TimedOut),
		attribute.Bool("hit", res.Hit),
		attribute.Float64("elapsed_seconds", res.Elapsed.Seconds()),
	)
	o.execDuration.Record(ctx, res.Elapsed.Seconds())
	if res.TimedOut {
		o.timeouts.Add(ctx, 1)
	}
	if runErr != nil {
		span.RecordError(runErr)
		span.SetStatus(codes.Error, runErr.Error())
	}
	slog.DebugContext(ctx, "agent exec finished",
		slog.String("dir", spec.Dir),
		slog.Int("exit_code", res.ExitCode),
		slog.Bool("timed_out", res.TimedOut),
		slog.Bool("hit", res.Hit),
		slog.Duration("elapsed", res.Elapsed),
		slog.String("stderr_tail", res.StderrTail))
}

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
	o := obs()
	ctx, span := o.tracer.Start(ctx, "evolve.agent.exec")
	defer span.End()

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	argv, err := e.Sandbox.wrap(spec.Dir, spec.Argv)
	if err != nil {
		return Result{}, startError(span, err)
	}

	slog.DebugContext(ctx, "agent exec started",
		slog.String("argv0", argv[0]),
		slog.String("dir", spec.Dir),
		slog.Duration("timeout", timeout))

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	configureProcessTreeKill(cmd)
	cmd.WaitDelay = waitDelay

	stderr := &ring{max: stderrTailBytes}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, startError(span, err)
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{}, startError(span, err)
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

	// Agents and the tools they invoke (terraform, linters, ...) emit ANSI
	// color codes; strip them here, at the one point all execution output is
	// captured, so the bytes that flow into graded evidence, retained logs,
	// and committed reports stay plain text. Stripping is a no-op on the
	// stream-json runners emit (there a tool's ANSI sits backslash-u
	// escaped inside a JSON string, not as a raw escape byte), so parsing
	// is unaffected.
	res := Result{
		Hit:        hit,
		Stdout:     []byte(ansi.Strip(collected.String())),
		StderrTail: ansi.Strip(stderr.String()),
		Elapsed:    time.Since(start),
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	switch {
	case ctx.Err() != nil:
		o.observe(ctx, span, spec, res, ctx.Err())
		return res, ctx.Err() // interrupted from above; abort the sweep
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		res.TimedOut = true
		o.observe(ctx, span, spec, res, nil)
		return res, nil
	default:
		// Runner exit codes are noise (headless CLIs exit non-zero on
		// max-turns, partial runs, ...); the output already tells the story.
		_ = waitErr
		o.observe(ctx, span, spec, res, nil)
		return res, nil
	}
}

// startError marks span errored for a run that never produced a Result (an
// unstartable command or a sandbox-wrap failure) and returns err unchanged.
func startError(span trace.Span, err error) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
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
