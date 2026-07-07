// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/runner"
	"github.com/bitwise-media-group/evolve/internal/tokencount"
)

// TestClearSelectionFlags pins the invariant the TUI relies on: once the form
// encodes the user's choice as an explicit per-model Filter, every selection
// flag must be cleared so the engine runs the selection verbatim. A new
// selection flag added without being cleared here would silently re-filter the
// form's picks (the bug that dropped --modified cases in the TUI).
func TestClearSelectionFlags(t *testing.T) {
	in := Options{New: true, Failed: true, Modified: true, SkillFilter: []string{"skill"}, Jobs: 4}
	got := in.ClearSelectionFlags()

	if got.New || got.Failed || got.Modified {
		t.Errorf("selection flags not all cleared: %+v", got)
	}
	// Non-selection fields are untouched, and the receiver is not mutated.
	if len(got.SkillFilter) != 1 || got.SkillFilter[0] != "skill" || got.Jobs != 4 {
		t.Errorf("unrelated fields changed: %+v", got)
	}
	if !in.New || !in.Failed || !in.Modified {
		t.Error("receiver mutated; method must return a copy")
	}
}

// TestCountTokens pins the fan-out helper that replaced the sequential per-case
// counting loop: counts must come back positionally (no scrambling under
// concurrency), a provider without the counting capability yields all-nil, and a
// non-positive Jobs budget must not deadlock the errgroup (the SetLimit(0) trap).
func TestCountTokens(t *testing.T) {
	// countingTriggerProvider.CountTokens returns len(text); distinct-length
	// texts make any positional scramble observable.
	texts := []string{"a", "bb", "ccc", "dddd", "eeeee", "ffffff", "ggggggg"}
	newCounter := func() *tokencount.Counter {
		return tokencount.New(filepath.Join(t.TempDir(), "c.json"), os.Stderr)
	}

	t.Run("counting provider maps positionally", func(t *testing.T) {
		p := &countingTriggerProvider{}
		opts := Options{Counter: newCounter(), CounterFor: fakeCounterFor(p), Jobs: 3} // fewer workers than texts
		sel := harness.Selection{Model: p.canonicalModel(), Harness: p}
		got := opts.countTokens(context.Background(), sel, texts)
		if len(got) != len(texts) {
			t.Fatalf("got %d counts, want %d", len(got), len(texts))
		}
		for i, text := range texts {
			if got[i] == nil {
				t.Errorf("count[%d] nil, want %d", i, len(text))
			} else if *got[i] != len(text) {
				t.Errorf("count[%d] = %d, want %d", i, *got[i], len(text))
			}
		}
	})

	t.Run("non-counting provider yields nils", func(t *testing.T) {
		p := &fakeTriggerProvider{} // no TokenCounter capability (cursor-like)
		opts := Options{Counter: newCounter(), Jobs: 4}
		sel := harness.Selection{Model: p.canonicalModel(), Harness: p}
		for i, c := range opts.countTokens(context.Background(), sel, texts) {
			if c != nil {
				t.Errorf("count[%d] = %d, want nil", i, *c)
			}
		}
	})

	t.Run("non-positive jobs does not deadlock", func(t *testing.T) {
		p := &countingTriggerProvider{}
		opts := Options{Counter: newCounter(), CounterFor: fakeCounterFor(p), Jobs: 0} // clamps to 1, not SetLimit(0)
		sel := harness.Selection{Model: p.canonicalModel(), Harness: p}
		if got := opts.countTokens(context.Background(), sel, texts); len(got) != len(texts) {
			t.Fatalf("got %d counts, want %d", len(got), len(texts))
		}
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		p := &countingTriggerProvider{}
		opts := Options{Counter: newCounter(), CounterFor: fakeCounterFor(p), Jobs: 4}
		sel := harness.Selection{Model: p.canonicalModel(), Harness: p}
		if got := opts.countTokens(context.Background(), sel, nil); len(got) != 0 {
			t.Errorf("got %d counts for empty input, want 0", len(got))
		}
	})
}

// TestOptionsSelects pins the --plugin/--skill filter semantics: an empty list
// matches everything, a non-empty list requires membership, and the two filters
// compose with AND.
func TestOptionsSelects(t *testing.T) {
	tests := []struct {
		name          string
		plugins       []string
		skills        []string
		plugin, skill string
		want          bool
	}{
		{"no filters match all", nil, nil, "p", "s", true},
		{"plugin in list", []string{"p", "q"}, nil, "p", "s", true},
		{"plugin not in list", []string{"q"}, nil, "p", "s", false},
		{"skill in list", nil, []string{"s"}, "p", "s", true},
		{"skill not in list", nil, []string{"t"}, "p", "s", false},
		{"both match", []string{"p"}, []string{"s"}, "p", "s", true},
		{"plugin matches but skill does not (AND)", []string{"p"}, []string{"t"}, "p", "s", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := Options{PluginFilter: tc.plugins, SkillFilter: tc.skills}
			if got := o.selects(tc.plugin, tc.skill); got != tc.want {
				t.Errorf("selects(%q, %q) = %v, want %v", tc.plugin, tc.skill, got, tc.want)
			}
		})
	}
}

// recordingRunner wraps another runner and counts invocations, so a test can
// assert an engine refused before spawning any agent.
type recordingRunner struct {
	inner Runner
	mu    sync.Mutex
	calls int
}

func (r *recordingRunner) Run(ctx context.Context, spec model.CommandSpec, timeout time.Duration, onLine func([]byte) bool) (runner.Result, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return r.inner.Run(ctx, spec, timeout, onLine)
}

// seedNewerSchema writes a results file claiming a future schema into the
// skill's results dir and returns its path and content for the untouched check.
func seedNewerSchema(t *testing.T, root, skill string) (string, []byte) {
	t.Helper()
	path := filepath.Join(root, "evals", skill, "results.json")
	content := []byte(`{"schema": 99, "models": {"m": {}}}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, content
}
