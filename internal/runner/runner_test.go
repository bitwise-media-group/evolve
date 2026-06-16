// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package runner

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bitwise-media-group/evolve/internal/provider"
)

func sh(script string) provider.CommandSpec {
	return provider.CommandSpec{Argv: []string{"/bin/sh", "-c", script}}
}

func TestRunCollectsStdout(t *testing.T) {
	res, err := (&Exec{}).Run(context.Background(), sh(`printf 'line1\nline2\n'`), 5*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(res.Stdout); got != "line1\nline2\n" {
		t.Errorf("stdout = %q", got)
	}
	if res.TimedOut || res.Hit {
		t.Errorf("res = %+v, want clean run", res)
	}
}

func TestRunSurfacesExitCode(t *testing.T) {
	// A non-zero exit is data the eval runtime-error classifier relies on, not
	// an error the runner should swallow or return.
	res, err := (&Exec{}).Run(context.Background(), sh(`echo out; exit 7`), 5*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", res.ExitCode)
	}
	if got := string(res.Stdout); got != "out\n" {
		t.Errorf("stdout = %q, want %q", got, "out\n")
	}
}

func TestRunScanHitExitsEarly(t *testing.T) {
	// The process would sleep for 60s after the hit line; the scan must kill
	// it as soon as the hit is seen.
	spec := sh(`echo nothing; echo HIT; sleep 60; echo late`)
	start := time.Now()
	res, err := (&Exec{}).Run(context.Background(), spec, 30*time.Second, func(line []byte) bool {
		return bytes.Contains(line, []byte("HIT"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Hit {
		t.Error("want hit")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("early exit took %s, want well under the sleep", elapsed)
	}
}

func TestRunTimeoutKillsProcessGroup(t *testing.T) {
	// The child ignores nothing, but it is a *grand*child via the subshell —
	// only a process-group kill reaps it promptly.
	spec := sh(`(sleep 60 & echo started; wait)`)
	start := time.Now()
	res, err := (&Exec{}).Run(context.Background(), spec, 500*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Error("want TimedOut")
	}
	if got := string(res.Stdout); !strings.Contains(got, "started") {
		t.Errorf("partial stdout = %q, want the pre-timeout output", got)
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Errorf("timeout kill took %s; grandchild likely held the pipe", elapsed)
	}
}

func TestRunStderrTailOnTimeout(t *testing.T) {
	spec := sh(`echo "rate limited by upstream" >&2; sleep 60`)
	res, err := (&Exec{}).Run(context.Background(), spec, 500*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || !strings.Contains(res.StderrTail, "rate limited") {
		t.Errorf("res = %+v, want stderr tail with the message", res)
	}
}

func TestRunParentCancelPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	_, err := (&Exec{}).Run(ctx, sh(`sleep 60`), 30*time.Second, nil)
	if err == nil {
		t.Error("want error on parent cancellation (Ctrl-C must abort the sweep)")
	}
}

func TestRunMissingBinary(t *testing.T) {
	spec := provider.CommandSpec{Argv: []string{"/nonexistent/definitely-missing"}}
	if _, err := (&Exec{}).Run(context.Background(), spec, time.Second, nil); err == nil {
		t.Error("want error for missing binary")
	}
}

func TestRunLongLines(t *testing.T) {
	// stream-json lines can exceed bufio.Scanner's 64 KiB default; the reader
	// must hand them to onLine intact (possibly in chunks, all scanned).
	spec := sh(`awk 'BEGIN{ s="x"; for (i=0; i<17; i++) s = s s; print s "NEEDLE" }'`)
	res, err := (&Exec{}).Run(context.Background(), spec, 10*time.Second, func(line []byte) bool {
		return bytes.Contains(line, []byte("NEEDLE"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Hit {
		t.Error("needle at the end of a 128 KiB line was not seen")
	}
}
