// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"errors"
	"log/slog"
)

// fanoutHandler dispatches each record to several slog handlers. evolve uses it
// to send every log line to both the stderr text handler (gated at the
// --verbose level) and the otelslog bridge (which accepts debug regardless), so
// the shipped telemetry file captures full diagnostics while stderr stays at the
// level the user chose. Each child is consulted with its own Enabled, so a debug
// record reaches the bridge even when stderr drops it.
type fanoutHandler struct {
	handlers []slog.Handler
}

// newFanoutHandler builds a handler over the given children.
func newFanoutHandler(handlers ...slog.Handler) slog.Handler {
	return &fanoutHandler{handlers: handlers}
}

// Enabled reports whether any child would handle a record at l, so the record
// is built whenever at least one destination wants it.
func (f *fanoutHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range f.handlers {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

// Handle forwards a clone to every child that accepts the record's level. The
// clone is per child because the otelslog bridge mutates the record it
// receives; errors from all children are joined so one failure cannot mask
// another.
func (f *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range f.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if err := h.Handle(ctx, r.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// WithAttrs returns a handler that applies attrs to every child.
func (f *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return &fanoutHandler{handlers: next}
}

// WithGroup returns a handler that opens the group on every child.
func (f *fanoutHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return f
	}
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithGroup(name)
	}
	return &fanoutHandler{handlers: next}
}
