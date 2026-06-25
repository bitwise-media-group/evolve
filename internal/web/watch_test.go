// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"testing"
	"time"

	"github.com/bitwise-media-group/evolve/internal/results"
)

func TestFingerprintChangesOnWrite(t *testing.T) {
	repo, resultsDir := fixtureRepo(t)
	s := NewServer(repo, "v1", nil)

	before := s.fingerprint()
	if before == "" {
		t.Fatal("fingerprint is empty for a repo with results")
	}

	// Append a trigger so the file's size and mtime change.
	f := sampleFile()
	f.Models["anthropic/claude-sonnet-4-6"].Triggers.Results = append(
		f.Models["anthropic/claude-sonnet-4-6"].Triggers.Results,
		results.TriggerResult{Query: "extra query", ShouldTrigger: true, Passed: new(true)},
	)
	if _, err := f.SaveDir(resultsDir, "json"); err != nil {
		t.Fatalf("rewrite results: %v", err)
	}
	if after := s.fingerprint(); after == before {
		t.Error("fingerprint unchanged after rewriting the results file")
	}
}

func TestWatchPublishesOnChange(t *testing.T) {
	repo, resultsDir := fixtureRepo(t)
	s := NewServer(repo, "v1", nil)
	ch := s.broker.subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Watch(ctx, 10*time.Millisecond)

	// Let Watch capture its baseline fingerprint before mutating.
	time.Sleep(50 * time.Millisecond)
	f := sampleFile()
	f.Models["anthropic/claude-sonnet-4-6"].Evals.Results = append(
		f.Models["anthropic/claude-sonnet-4-6"].Evals.Results,
		results.EvalResult{ID: "extra", Passed: new(false)},
	)
	if _, err := f.SaveDir(resultsDir, "json"); err != nil {
		t.Fatalf("rewrite results: %v", err)
	}

	select {
	case got := <-ch:
		if got != "results-changed" {
			t.Errorf("got %q, want results-changed", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not publish after the results file changed")
	}
}
