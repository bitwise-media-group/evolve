// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package plan

import (
	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/model"
)

// CaseReason categorizes, for one (model, case) pair, why a rerun might select
// it: New (no/incomplete stored data a rerun could fill), Modified (authored
// content or case definition changed), Failing (a complete result that did not
// pass or errored). A pair absent from a Reasons map is not applicable to that
// model (e.g. the eval-set models restriction excludes it).
type CaseReason struct {
	New      bool
	Modified bool
	Failing  bool
}

// Reasons holds the per-model, per-case categories the filter toggles act on,
// keyed by model key then CaseRef. It is computed once from committed results
// (see run.CaseReasons) and handed to a Session; the Session owns the filter and
// selection logic so the form is a thin driver and the engine and dashboard run
// the identical resolved Plan.
type Reasons map[string]map[CaseRef]CaseReason

// Filters is the new/modified/failed toggle state. With none set, every
// applicable case is queued (a plain run); with any set, an auto case is queued
// only when one enabled filter matches its reason.
type Filters struct {
	New      bool
	Modified bool
	Failed   bool
}

func (f Filters) any() bool { return f.New || f.Modified || f.Failed }

// HarnessState pairs a configured harness with whether its CLI is on PATH. The
// harness pane shows unavailable harnesses greyed; a model whose only harnesses
// are unavailable cannot run.
type HarnessState struct {
	Harness   harness.Harness
	Available bool
	// Models is the offered-models probe result for this harness (see
	// harness.OfferedModels): tokens naming the models the operator's installed
	// CLI actually serves. nil means unknown — no probe ran, the harness has no
	// listing surface, or the probe failed — and fails open: every supported
	// model counts as offered.
	Models []string
}

// NodeSel is the resolved selection state of a tree node (a case, or a
// plugin/skill aggregating its cases), the single semantic the form renders as a
// glyph. Auto* states follow the filter baseline; the form distinguishes them so
// it can show "queued for all / some / none" without re-deriving the plan.
type NodeSel int

const (
	SelForceOff    NodeSel = iota // user forced off (dirty)
	SelForceOn                    // user forced on (dirty) — runs for every enabled model
	SelAutoAll                    // auto: queued for every applicable enabled model
	SelAutoPartial                // auto: queued for some but not all
	SelAutoNone                   // auto: queued for none
)

// Session is the stateful owner of a run's filter and selection intent. The form
// mutates it through receivers and re-renders from its derived queries; Plan()
// projects the current state into the canonical Plan the engine executes, so the
// form preview, the dashboard, and the engine cannot drift.
type Session struct {
	cat       []SkillCatalog
	models    []model.Model  // canonical models, in display order
	harnesses []HarnessState // configured harnesses, in display order
	prior     PriorMetrics
	reasons   Reasons

	filters   Filters
	harnessOn map[string]bool   // harness id -> enabled
	modelOn   map[string]bool   // model key -> enabled
	cases     map[CaseRef]State // force-on/off; absent = Partial (auto)
}

// NewSession builds a session. enabledHarnessIDs and enabledModelKeys seed the
// initial enable state (typically the harnesses/models a --harness/--model run
// would target); filters seed the new/modified/failed toggles. Cases all start
// auto.
func NewSession(
	cat []SkillCatalog, models []model.Model, harnesses []HarnessState,
	prior PriorMetrics, reasons Reasons,
	filters Filters, enabledHarnessIDs, enabledModelKeys []string,
) *Session {
	s := &Session{
		cat: cat, models: models, harnesses: harnesses, prior: prior, reasons: reasons,
		filters:   filters,
		harnessOn: map[string]bool{},
		modelOn:   map[string]bool{},
		cases:     map[CaseRef]State{},
	}
	// Seed every displayed harness/model explicitly so enabled state is a plain
	// map lookup (absent never means "on").
	for _, h := range harnesses {
		s.harnessOn[h.Harness.ID()] = false
	}
	for _, m := range models {
		s.modelOn[m.Key()] = false
	}
	for _, id := range enabledHarnessIDs {
		s.harnessOn[id] = true
	}
	for _, k := range enabledModelKeys {
		s.modelOn[k] = true
	}
	// A model the resolved harness's CLI does not offer the operator starts
	// deselected: it stays toggleable (the CLI may still accept it explicitly),
	// but a default run should span only models the account actually serves.
	for _, m := range models {
		if s.modelOn[m.Key()] && !s.ModelOffered(m) {
			s.modelOn[m.Key()] = false
		}
	}
	return s
}

// --- filter receivers ---

// SetNewFilter toggles the "new" filter.
func (s *Session) SetNewFilter(on bool) { s.filters.New = on }

// SetModifiedFilter toggles the "modified" filter.
func (s *Session) SetModifiedFilter(on bool) { s.filters.Modified = on }

// SetFailedFilter toggles the "failed" filter.
func (s *Session) SetFailedFilter(on bool) { s.filters.Failed = on }

// FilterState returns the current filter toggles.
func (s *Session) FilterState() Filters { return s.filters }

// --- harness / model receivers ---

// EnableHarness enables or disables a harness. Disabling one drops the models it
// would have driven (a secondary filter); it does not auto-enable any case.
func (s *Session) EnableHarness(id string, on bool) { s.harnessOn[id] = on }

// EnableModel enables or disables a model. Disabling one drops it from the run; it
// does not auto-enable any case.
func (s *Session) EnableModel(key string, on bool) { s.modelOn[key] = on }

// --- node receivers ---

// SetCases sets every given case to st (On = force on, Off = force off, Partial =
// auto). Plugin/skill toggles expand to their cases and call this.
func (s *Session) SetCases(refs []CaseRef, st State) {
	for _, cr := range refs {
		if st == Partial {
			delete(s.cases, cr)
		} else {
			s.cases[cr] = st
		}
	}
}

// --- display queries ---

// Harnesses returns the configured harnesses with availability, in order.
func (s *Session) Harnesses() []HarnessState { return s.harnesses }

// HarnessEnabled reports whether harness id is currently enabled.
func (s *Session) HarnessEnabled(id string) bool { return s.harnessOn[id] }

// Models returns the canonical models, in display order.
func (s *Session) Models() []model.Model { return s.models }

// ModelEnabled reports whether the model key is currently enabled.
func (s *Session) ModelEnabled(key string) bool { return s.modelOn[key] }

// ModelRunnable reports whether the model has at least one enabled, available
// harness — i.e. selecting it would actually run it. The model pane greys models
// that are not runnable under the current harness selection.
func (s *Session) ModelRunnable(m model.Model) bool {
	_, ok := harness.RunnableHarness(m, s.eligible())
	return ok
}

// ModelOffered reports whether the harness that would run m under the current
// selection offers it to the operator's account, per that harness's
// offered-models probe (HarnessState.Models). Unknown — no probe result, or no
// resolvable harness — is true, so a failed or absent probe never hides a
// model; ModelRunnable separately covers the no-harness case.
func (s *Session) ModelOffered(m model.Model) bool {
	id, ok := harness.RunnableHarness(m, s.eligible())
	if !ok {
		return true
	}
	for _, h := range s.harnesses {
		if h.Harness.ID() == id {
			return h.Models == nil || harness.OffersModel(m, id, h.Models)
		}
	}
	return true
}

// AnyQueued reports whether the resolved plan would run at least one case.
func (s *Session) AnyQueued() bool {
	for _, pl := range s.Plan().Plugins {
		for _, sk := range pl.Skills {
			for _, m := range sk.Models {
				for _, u := range m.Units {
					for _, c := range u.Cases {
						if c.Queued {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// NodeSel aggregates the selection state of the given cases across the enabled
// models: a forced state when every case shares it, otherwise the auto state
// reflecting how many applicable enabled models are queued.
func (s *Session) NodeSel(refs []CaseRef) NodeSel {
	if len(refs) == 0 {
		return SelAutoNone
	}
	allForceOn, allForceOff := true, true
	for _, cr := range refs {
		switch s.cases[cr] {
		case On:
			allForceOff = false
		case Off:
			allForceOn = false
		default:
			allForceOn, allForceOff = false, false
		}
	}
	switch {
	case allForceOn:
		return SelForceOn
	case allForceOff:
		return SelForceOff
	}
	// Auto (or mixed): classify by how many applicable enabled (model, case) pairs
	// the baseline queues.
	queued, total := s.queuedCounts(refs)
	switch {
	case total == 0 || queued == 0:
		return SelAutoNone
	case queued == total:
		return SelAutoAll
	default:
		return SelAutoPartial
	}
}

// AutoAvailable reports whether the auto state would queue at least one
// (model, case) pair under the node — the gate for offering the auto cycle slot.
func (s *Session) AutoAvailable(refs []CaseRef) bool {
	queued, _ := s.queuedCounts(refs)
	return queued > 0
}

// queuedCounts counts, over the node's cases and the enabled+runnable models the
// case applies to, how many the baseline (filters over reasons) queues, treating
// a force-on case as always queued and a force-off case as never.
func (s *Session) queuedCounts(refs []CaseRef) (queued, total int) {
	enabled := s.enabledModelKeys()
	for _, cr := range refs {
		for _, mk := range enabled {
			rm, ok := s.reasons[mk]
			if !ok {
				continue
			}
			if _, applicable := rm[cr]; !applicable {
				continue // case not applicable to this model (eval-set models restriction)
			}
			total++
			switch s.cases[cr] {
			case On:
				queued++
			case Off:
				// never queued
			default:
				if s.baseline(mk, cr) {
					queued++
				}
			}
		}
	}
	return queued, total
}

// --- resolution ---

// Plan projects the current state into the canonical Plan the engine runs.
func (s *Session) Plan() Plan {
	return Build(s.cat, s.enabledSelections(), s.selection(), s.prior)
}

// Selection is the resolved plan.Selection for the current state, for the
// RunRequest the engine and dashboard re-Build from (the same inputs Plan uses,
// so all three agree).
func (s *Session) Selection() Selection { return s.selection() }

// EnabledSelections is the (model, harness) pairs the run will span, in display
// order — the RunRequest's model list.
func (s *Session) EnabledSelections() []harness.Selection { return s.enabledSelections() }

// eligible is the set of harness ids that are enabled and available on PATH.
func (s *Session) eligible() map[string]bool {
	out := map[string]bool{}
	for _, h := range s.harnesses {
		if h.Available && s.harnessOn[h.Harness.ID()] {
			out[h.Harness.ID()] = true
		}
	}
	return out
}

// harnessByID indexes the configured harnesses.
func (s *Session) harnessByID() map[string]harness.Harness {
	out := map[string]harness.Harness{}
	for _, h := range s.harnesses {
		out[h.Harness.ID()] = h.Harness
	}
	return out
}

// enabledSelections binds each enabled, runnable model to a harness from the
// eligible set, in display order. A model that is disabled or has no eligible
// harness is omitted, so Build never produces cases for it.
func (s *Session) enabledSelections() []harness.Selection {
	eligible := s.eligible()
	byID := s.harnessByID()
	var out []harness.Selection
	for _, m := range s.models {
		if !s.modelOn[m.Key()] {
			continue
		}
		id, ok := harness.RunnableHarness(m, eligible)
		if !ok {
			continue
		}
		out = append(out, harness.Selection{Model: m, Harness: byID[id]})
	}
	return out
}

// enabledModelKeys is the keys of enabledSelections, for the queued-count queries.
func (s *Session) enabledModelKeys() []string {
	sels := s.enabledSelections()
	out := make([]string, 0, len(sels))
	for _, sel := range sels {
		out = append(out, sel.Key())
	}
	return out
}

// baseline reports whether the filter/reason baseline queues (mk, cr): every
// applicable case when no filter is set, else any enabled filter that matches.
func (s *Session) baseline(mk string, cr CaseRef) bool {
	r, ok := s.reasons[mk][cr]
	if !ok {
		return false // not applicable to this model
	}
	if !s.filters.any() {
		return true
	}
	return (s.filters.New && r.New) ||
		(s.filters.Modified && r.Modified) ||
		(s.filters.Failed && r.Failing)
}

// selection builds the plan.Selection for the current state: every enabled model
// follows the baseline (Partial), each case carries its force-on/off override,
// and Needs is the filter/reason baseline per enabled model.
func (s *Session) selection() Selection {
	sel := Selection{
		Models: map[string]State{},
		Cases:  map[CaseRef]State{},
		Needs:  map[string]map[CaseRef]bool{},
	}
	for cr, st := range s.cases {
		sel.Cases[cr] = st
	}
	for _, mk := range s.enabledModelKeys() {
		need := map[CaseRef]bool{}
		for cr := range s.reasons[mk] {
			need[cr] = s.baseline(mk, cr)
		}
		sel.Needs[mk] = need
	}
	return sel
}
