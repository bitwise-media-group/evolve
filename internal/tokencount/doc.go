// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package tokencount caches provider-reported input-token counts. Counts come
// from each provider's official counting API — never a local tokenizer (they
// miscount non-native models by 15-20%+) — and are cached on disk keyed by
// sha256(provider, model, payload), so re-runs and report regeneration cost
// nothing.
package tokencount
