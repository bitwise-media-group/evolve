// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package schemas embeds the published JSON Schemas (draft 2020-12) that
// define evolve's authored eval files (evals, triggers) and emitted
// artifacts (results, EVALUATION rollup), plus the skill-creator-compatible
// contracts they extend (grading, metrics, timing, benchmark, history).
// The conformance tests in this package are the superset guarantee: the
// verbatim examples from skill-creator's references/schemas.md must
// validate, as must evolve's own marshaled output.
package schemas

import "embed"

// FS holds every *.schema.json, addressable by file name.
//
//go:embed *.schema.json
var FS embed.FS
