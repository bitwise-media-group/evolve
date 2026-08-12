// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import (
	"encoding/json"
	"fmt"

	"github.com/bitwise-media-group/evolve/internal/results"
)

// evidenceCap is what one graded assertion's evidence shrinks to when the
// entry must lose weight.
const evidenceCap = 512

// MarshalTriggerEntry renders a trigger entry within MaxEntryBytes,
// dropping the previous snapshot's per-query results first — the summary
// survives, and the wire's fidelity floor is the fresh results.
func MarshalTriggerEntry(e *results.TriggerEntry) (json.RawMessage, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("remote: marshal trigger entry: %w", err)
	}
	if len(raw) <= MaxEntryBytes {
		return raw, nil
	}
	if e.Previous != nil {
		e.Previous.Results = nil
	}
	raw, err = json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("remote: marshal trigger entry: %w", err)
	}
	if len(raw) > MaxEntryBytes {
		return nil, fmt.Errorf("remote: trigger entry is %d bytes even without snapshots (cap %d)",
			len(raw), MaxEntryBytes)
	}
	return raw, nil
}

// MarshalEvalEntry renders an eval entry within MaxEntryBytes, shedding
// weight in fidelity order: snapshot result arrays first (their summaries
// survive), then assertion evidence, then the assertions themselves.
func MarshalEvalEntry(e *results.EvalEntry) (json.RawMessage, error) {
	shrinks := []func(){
		func() {
			if e.Previous != nil {
				e.Previous.Results = nil
			}
			if e.Baseline != nil {
				e.Baseline.Results = nil
			}
		},
		func() {
			for i := range e.Results {
				for j := range e.Results[i].Expectations {
					exp := &e.Results[i].Expectations[j]
					exp.Evidence = Truncate(exp.Evidence, evidenceCap)
				}
			}
		},
		func() {
			for i := range e.Results {
				e.Results[i].Expectations = nil
			}
		},
	}
	for {
		raw, err := json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("remote: marshal eval entry: %w", err)
		}
		if len(raw) <= MaxEntryBytes {
			return raw, nil
		}
		if len(shrinks) == 0 {
			return nil, fmt.Errorf("remote: eval entry is %d bytes even fully trimmed (cap %d)",
				len(raw), MaxEntryBytes)
		}
		shrinks[0]()
		shrinks = shrinks[1:]
	}
}
