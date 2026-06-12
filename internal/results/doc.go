// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package results owns the committed results.json files that live beside
// each skill's eval definitions (evals/<skill>/results.json).
//
// One file per skill holds both the triggers and cases sections, keyed by
// "provider/model-id" (provider-qualified because Cursor runs other vendors'
// models, so bare ids could collide). Optional usage is grouped and omitted,
// not nulled: providers without counting or usage reporting simply lack the
// estimate/measured sub-objects. Pricing is snapshotted per entry — possibly
// an explicit null — so reports can distinguish "not measured yet" from "can
// never be measured" without consulting the live model matrix.
//
// Determinism matters because these files are committed: 2-space indent plus
// trailing newline, map keys sorted (encoding/json does that), struct field
// order fixed by declaration, RFC3339 UTC timestamps, costs rounded to 6
// decimals and seconds to 1 before marshaling.
package results
