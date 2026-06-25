// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bitwise-media-group/evolve/internal/results"
)

// defaultWatchInterval is how often Watch polls the results files for changes.
const defaultWatchInterval = time.Second

// Watch polls the repository's results files and publishes a "results-changed"
// notification whenever any is written, added, or removed, so any run — CLI,
// TUI, or CI — that rewrites them refreshes an open browser. It blocks until ctx
// is cancelled. interval <= 0 uses [defaultWatchInterval].
func (s *Server) Watch(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultWatchInterval
	}
	last := s.fingerprint()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if fp := s.fingerprint(); fp != last {
				last = fp
				s.broker.publish()
			}
		}
	}
}

// fingerprint summarises every results file's path, mtime, and size into one
// string; any change to the set or contents changes the fingerprint. Errors are
// skipped so a transient read failure does not spuriously fire.
func (s *Server) fingerprint() string {
	sets, err := s.repo.EvalSets()
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, set := range sets {
		path := results.Find(set.ResultsDir)
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "%s\x00%d\x00%d\n", path, info.ModTime().UnixNano(), info.Size())
	}
	return b.String()
}
