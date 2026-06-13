// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package results owns the committed results files that live beside each
// skill's eval definitions (evals/<skill>/results.<ext>, format selected by
// results_format).
//
// One file per skill holds both the triggers and evals sections, keyed by
// "provider/model-id" (provider-qualified because Cursor runs other vendors'
// models, so bare ids could collide). The per-eval result is a superset of
// skill-creator's grading.json: an expectations array whose entries carry
// text/passed/evidence (plus the authored assertion echoed alongside), a
// summary with grading.json's field names, and a timing block — so tooling
// written against skill-creator output reads evolve results unchanged.
// Optional usage is grouped and omitted, not nulled: providers without
// counting or usage reporting simply lack the estimate/measured sub-objects.
// Pricing is snapshotted per entry — possibly an explicit null — so reports
// can distinguish "not measured yet" from "can never be measured" without
// consulting the live model matrix. Readers treat an absent key and an
// explicit null identically.
//
// Determinism matters because these files are committed: 2-space indent plus
// trailing newline, map keys sorted, struct field order fixed by
// declaration, RFC3339 UTC timestamps, costs rounded to 6 decimals and
// seconds to 1 before marshaling.
package results
