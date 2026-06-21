// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tokencount

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/bitwise-media-group/evolve/internal/provider"
)

// scopeName is this package's OpenTelemetry instrumentation scope.
const scopeName = "github.com/bitwise-media-group/evolve/internal/tokencount"

func tracer() trace.Tracer { return otel.Tracer(scopeName) }

// Counter wraps the providers' counting APIs with a persistent cache and
// warn-once diagnostics. It is safe for concurrent use.
type Counter struct {
	mu     sync.Mutex
	cache  map[string]int
	path   string
	dirty  bool
	warned map[string]bool
	stderr io.Writer
}

// DefaultCachePath is the user-scoped cache location, so the tool works
// against any repository without committing a hash dump to it.
func DefaultCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "evolve", "token-counts.json"), nil
}

// New loads the cache at path (a corrupt or missing cache starts empty).
func New(path string, stderr io.Writer) *Counter {
	c := &Counter{
		cache:  map[string]int{},
		path:   path,
		warned: map[string]bool{},
		stderr: stderr,
	}
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &c.cache) != nil {
			c.cache = map[string]int{}
		}
	}
	return c
}

// Count returns the provider-reported input-token count for text, or nil when
// the provider has no counting API, no credential is set, or the call fails.
func (c *Counter) Count(ctx context.Context, p provider.Provider, modelID, text string) *int {
	tc, ok := p.(provider.TokenCounter)
	if !ok {
		return nil // capability absent (e.g. cursor) — expected, no warning
	}

	ctx, span := tracer().Start(ctx, "evolve.tokencount", trace.WithAttributes(
		attribute.String("provider", p.Name()),
		attribute.String("model", modelID),
	))
	defer span.End()

	digest := sha256.Sum256([]byte(p.Name() + "\x00" + modelID + "\x00" + text))
	key := hex.EncodeToString(digest[:])
	c.mu.Lock()
	cached, hit := c.cache[key]
	c.mu.Unlock()
	if hit {
		span.SetAttributes(attribute.Bool("cache_hit", true), attribute.Int("token_count", cached))
		return &cached
	}
	span.SetAttributes(attribute.Bool("cache_hit", false))

	tokens, err := tc.CountTokens(ctx, modelID, text)
	switch {
	case errors.Is(err, provider.ErrNoCredential):
		slog.DebugContext(ctx, "token count skipped: no credential",
			slog.String("provider", p.Name()),
			slog.String("model", modelID))
		c.warn(p.Name(), "no API key or OAuth token set; token counts omitted")
		return nil
	case err != nil:
		slog.DebugContext(ctx, "token count failed",
			slog.String("provider", p.Name()),
			slog.String("model", modelID),
			slog.Any("error", err))
		recordSpanErr(span, err)
		c.warn(p.Name()+"/"+modelID, fmt.Sprintf("count_tokens failed: %v", err))
		return nil
	}

	span.SetAttributes(attribute.Int("token_count", tokens))
	c.mu.Lock()
	c.cache[key] = tokens
	c.dirty = true
	c.mu.Unlock()
	return &tokens
}

// recordSpanErr marks span errored.
func recordSpanErr(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

func (c *Counter) warn(scope, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.warned[scope] {
		c.warned[scope] = true
		fmt.Fprintf(c.stderr, "  warn: [%s] %s\n", scope, message)
	}
}

// Save writes the cache atomically. A no-op when nothing new was counted.
func (c *Counter) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.cache, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return err
	}
	c.dirty = false
	return nil
}
