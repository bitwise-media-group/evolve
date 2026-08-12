// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package remote is evolve's client for patchy's remote-evaluation service:
// the OIDC login flow and credential store, the HTTP/SSE client, the
// deterministic workspace bundler, the remote sweep orchestrator, and the
// bidirectional Reporter seam — EventReporter serializes run.Reporter calls
// onto the in-pod EVOLVE-EVENT stdout stream, ApplyEvent replays received
// events back onto a local Reporter, so a remote run's output is
// indistinguishable from a local one.
//
// The wire types in wire.go are local copies of patchy's pkg/evaluation
// contract; the import swaps to the published package once patchy releases
// it (they must stay field-for-field identical until then).
package remote
