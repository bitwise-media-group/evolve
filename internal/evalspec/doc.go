// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package evalspec parses the authored eval definitions: triggers.json and
// cases.json. The formats are byte-compatible with the Python harness's, plus
// optional extensions (skip_providers, per-case timeout_seconds) that default
// to the original behavior when absent.
package evalspec
