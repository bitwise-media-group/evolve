// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package encfmt reads and writes the supported encoding formats — JSON,
// JSONC, and YAML — behind one JSON data model. Files decode by extension
// into json-tagged structs (JSONC standardized via hujson, YAML decoded
// generically and re-marshaled), so the json struct tags stay the single
// source of truth and no yaml tags exist anywhere. Emission runs the same
// round-trip in reverse, keeping integers integral and explicit nulls
// intact across formats.
package encfmt
