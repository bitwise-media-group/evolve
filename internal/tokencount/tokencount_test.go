// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tokencount

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/provider"
)

// fakeProvider implements Provider and optionally TokenCounter.
type fakeProvider struct {
	provider.Provider
	count func(model, text string) (int, error)
	calls int
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) CountTokens(_ context.Context, model, text string) (int, error) {
	f.calls++
	return f.count(model, text)
}

type noCountProvider struct{ provider.Provider }

func (noCountProvider) Name() string { return "cursorish" }

func TestCountCachesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "token-counts.json")
	var stderr bytes.Buffer
	c := New(path, &stderr)
	p := &fakeProvider{count: func(_, _ string) (int, error) { return 42, nil }}

	for range 3 {
		if got := c.Count(context.Background(), p, "m1", "text"); got == nil || *got != 42 {
			t.Fatalf("Count = %v, want 42", got)
		}
	}
	if p.calls != 1 {
		t.Errorf("API calls = %d, want 1 (cached afterwards)", p.calls)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	// A fresh counter reloads the persisted entry without calling the API.
	c2 := New(path, &stderr)
	p2 := &fakeProvider{count: func(_, _ string) (int, error) { t.Fatal("unexpected API call"); return 0, nil }}
	if got := c2.Count(context.Background(), p2, "m1", "text"); got == nil || *got != 42 {
		t.Fatalf("reloaded Count = %v, want 42", got)
	}
}

func TestCountKeyIncludesProviderAndModel(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "c.json"), os.Stderr)
	p := &fakeProvider{count: func(model, _ string) (int, error) {
		if model == "m1" {
			return 1, nil
		}
		return 2, nil
	}}
	if got := c.Count(context.Background(), p, "m1", "text"); *got != 1 {
		t.Errorf("m1 = %d, want 1", *got)
	}
	if got := c.Count(context.Background(), p, "m2", "text"); *got != 2 {
		t.Errorf("m2 = %d, want 2 (distinct cache key per model)", *got)
	}
}

func TestCountNoCapability(t *testing.T) {
	var stderr bytes.Buffer
	c := New(filepath.Join(t.TempDir(), "c.json"), &stderr)
	if got := c.Count(context.Background(), noCountProvider{}, "m", "text"); got != nil {
		t.Errorf("Count = %v, want nil for a provider without counting", *got)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected warning for capability absence: %q", stderr.String())
	}
}

func TestCountWarnsOnce(t *testing.T) {
	var stderr bytes.Buffer
	c := New(filepath.Join(t.TempDir(), "c.json"), &stderr)
	p := &fakeProvider{count: func(_, _ string) (int, error) { return 0, provider.ErrNoCredential }}
	c.Count(context.Background(), p, "m", "a")
	c.Count(context.Background(), p, "m", "b")
	if got := bytes.Count(stderr.Bytes(), []byte("warn:")); got != 1 {
		t.Errorf("warnings = %d, want 1\n%s", got, stderr.String())
	}
}

func TestCountAPIFailure(t *testing.T) {
	var stderr bytes.Buffer
	c := New(filepath.Join(t.TempDir(), "c.json"), &stderr)
	p := &fakeProvider{count: func(_, _ string) (int, error) { return 0, errors.New("boom") }}
	if got := c.Count(context.Background(), p, "m", "a"); got != nil {
		t.Errorf("Count = %v, want nil on API failure", *got)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("count_tokens failed: boom")) {
		t.Errorf("missing failure warning: %q", stderr.String())
	}
}

func TestCorruptCacheStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(path, os.Stderr)
	p := &fakeProvider{count: func(_, _ string) (int, error) { return 9, nil }}
	if got := c.Count(context.Background(), p, "m", "t"); got == nil || *got != 9 {
		t.Fatalf("Count = %v, want 9", got)
	}
}
