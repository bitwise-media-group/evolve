// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package plan

// State is a node's selection intent. Partial (the zero value) follows the
// preselect baseline (Needs) — what a --new/--failed/--modified sweep seeds, and
// the default for any node the form has not explicitly toggled; Off excludes the
// node; On widens it to run everywhere it applies (every selected model, or every
// selected case). The form mutates these as the user toggles rows.
type State int

const (
	Partial State = iota // zero value: follow the Needs baseline
	Off
	On
)

// Selection is the user's enable/disable intent over the full provider×case
// matrix, plus the preselect baseline. It is what the selection form mutates and
// submits, and the single input Build resolves into a Plan. A model or case absent
// from the maps is Partial — it follows the Needs baseline.
type Selection struct {
	Models map[string]State            // model key ("provider/model") -> state
	Cases  map[CaseRef]State           // authored case -> state
	Needs  map[string]map[CaseRef]bool // model key -> case -> preselected by the sweep flags
}

// queued resolves whether case cr runs for model mk this session — the rule the
// engine and form share: both ends must be on (not Off), and the case runs if
// either end is fully On (the user widened it) or the sweep preselected it
// (Needs). Applicability (skip_providers, the tier/skill filters) is enforced by
// Build before it consults this, so queued never re-checks it.
func (s Selection) queued(mk string, cr CaseRef) bool {
	if s.Models[mk] == Off || s.Cases[cr] == Off {
		return false
	}
	return s.Models[mk] == On || s.Cases[cr] == On || s.Needs[mk][cr]
}
