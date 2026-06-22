// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package profile turns the hidden --profile flag into pprof output. It exists so
// runtime/pprof stays out of cmd/evolve and the set of supported profiles is one
// list to extend.
//
// Parse resolves the flag's values (cpu, memory, or all — repeated or
// comma-separated, already flattened by cobra's StringSlice) into a deduped,
// canonically ordered set of Kinds. Start begins those profiles and returns a
// StopFunc the caller runs once the profiled work is done: a streaming profile
// (CPU) runs until Stop, while a snapshot profile (memory) is captured at Stop so
// it reflects the run's live set rather than startup. Each profile writes one
// <kind>.pprof file, ready for `go tool pprof`.
//
// Off by default: an empty Parse yields no Kinds and Start returns a nil StopFunc.
package profile
