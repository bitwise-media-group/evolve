// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"log/slog"
	"testing"
)

func TestFanoutDispatchesToEveryChild(t *testing.T) {
	a := newCaptureHandler(slog.LevelDebug)
	b := newCaptureHandler(slog.LevelDebug)
	logger := slog.New(newFanoutHandler(a, b))

	logger.Info("hello", slog.String("k", "v"))

	if a.count() != 1 || b.count() != 1 {
		t.Fatalf("want both children to receive 1 record, got a=%d b=%d", a.count(), b.count())
	}
}

func TestFanoutGatesPerChildLevel(t *testing.T) {
	// Mirrors the real setup: a stderr-like child at info and a file-like child at
	// debug. A debug record must reach only the debug child, an info record both.
	stderr := newCaptureHandler(slog.LevelInfo)
	file := newCaptureHandler(slog.LevelDebug)
	logger := slog.New(newFanoutHandler(stderr, file))

	logger.Debug("debug line")
	if stderr.count() != 0 {
		t.Errorf("info-gated child got a debug record: %d", stderr.count())
	}
	if file.count() != 1 {
		t.Errorf("debug-gated child missed the debug record: %d", file.count())
	}

	logger.Info("info line")
	if stderr.count() != 1 || file.count() != 2 {
		t.Errorf("info record routing wrong: stderr=%d file=%d", stderr.count(), file.count())
	}
}

func TestFanoutWithAttrsAndGroupPropagate(t *testing.T) {
	a := newCaptureHandler(slog.LevelDebug)
	b := newCaptureHandler(slog.LevelDebug)
	h := newFanoutHandler(a, b).WithAttrs([]slog.Attr{slog.String("svc", "evolve")}).WithGroup("g")

	// Both children should still receive records after WithAttrs/WithGroup.
	if err := h.Handle(context.Background(), slog.Record{Level: slog.LevelInfo}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if a.count() != 1 || b.count() != 1 {
		t.Fatalf("WithAttrs/WithGroup dropped a child: a=%d b=%d", a.count(), b.count())
	}
}

func TestFanoutEnabledReflectsChildren(t *testing.T) {
	stderr := newCaptureHandler(slog.LevelInfo)
	file := newCaptureHandler(slog.LevelDebug)
	h := newFanoutHandler(stderr, file)
	ctx := context.Background()

	// Debug: only the file child wants it, but the fanout must still be enabled so
	// the record is built and reaches that child.
	if !h.Enabled(ctx, slog.LevelDebug) {
		t.Error("fanout should be enabled when any child accepts the level")
	}
	// A level below every child is not enabled.
	if h.Enabled(ctx, slog.LevelDebug-4) {
		t.Error("fanout should be disabled when no child accepts the level")
	}
}
