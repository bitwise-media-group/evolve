// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/runner"
)

// probeRunner is the minimal runner surface a model probe needs; *runner.Exec
// satisfies it, tests fake it.
type probeRunner interface {
	Run(ctx context.Context, spec model.CommandSpec, timeout time.Duration,
		scan *runner.Scan) (runner.Result, error)
}

// ProbeOfferedModels asks every available harness that can report its offered
// models (harness.OfferedModels) what the operator's installed CLI actually
// serves, concurrently, each probe bounded by timeout. The result maps harness
// id to offered tokens; a harness without the capability, off PATH, or whose
// probe failed or reported nothing is absent — unknown, which callers must
// treat as "everything offered" (fail open) so a transient probe failure never
// silently deselects a run. Probes execute unconfined (they only list; the
// CLIs write their own operator-side state, which a workspace sandbox would
// break) — pass a zero runner.Exec.
func ProbeOfferedModels(ctx context.Context, r *runner.Exec, harnesses []harness.Harness,
	timeout time.Duration) map[string][]string {
	out := map[string][]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, h := range harnesses {
		lister, ok := h.(harness.OfferedModels)
		if !ok {
			continue
		}
		if _, onPath := harness.Available(h); !onPath {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			offered, err := lister.ListOfferedModels(ctx, probeExec(r, h, timeout))
			if err != nil || len(offered) == 0 {
				slog.DebugContext(ctx, "offered-models probe yielded nothing",
					slog.String("harness", h.ID()), slog.Any("error", err))
				return
			}
			mu.Lock()
			out[h.ID()] = offered
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// probeExec adapts the runner into the harness.ProbeExec contract for one
// harness: Argv[0] is resolved to the installed CLI, and a done predicate is
// wired through scan mode so a probe against a server-style CLI (codex
// app-server) is killed as soon as its response line arrives while its output
// is still collected.
func probeExec(r probeRunner, h harness.Harness, timeout time.Duration) harness.ProbeExec {
	return func(ctx context.Context, spec model.CommandSpec, done func(line []byte) bool) ([]byte, error) {
		cli, ok := harness.Available(h)
		if !ok {
			return nil, fmt.Errorf("%s CLI not on PATH", h.ID())
		}
		spec.Argv[0] = cli
		if done == nil {
			res, err := r.Run(ctx, spec, timeout, nil)
			return res.Stdout, err
		}
		var buf bytes.Buffer
		res, err := r.Run(ctx, spec, timeout, &runner.Scan{OnLine: func(line []byte) bool {
			buf.Write(line)
			return done(line)
		}})
		if err != nil {
			return nil, err
		}
		if !res.Hit {
			return nil, fmt.Errorf("%s probe ended before its response arrived", h.ID())
		}
		return buf.Bytes(), nil
	}
}
