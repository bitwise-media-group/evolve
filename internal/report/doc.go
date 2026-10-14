// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package report renders the committed results.json files into EVALUATION.md
// (root rollup, plus per-plugin detail pages in marketplace/multi layouts)
// and a machine-readable EVALUATION.json.
//
// Rendering distinguishes two kinds of empty cell, decidable from stored
// data plus provider capabilities: "—" means not measured yet (a rerun could
// fill it), "n/a" means the provider can never produce the figure (no
// counting API, no usage reporting, or no published pricing snapshot).
//
// An agent repository renders as one root report: its agents group under an
// "Agents" heading with protocol/endpoint columns, each agent's case tables
// follow in the same file, and the group gates as stable. Under --strict each
// agent must have results for every endpoint its own suite defines.
package report
