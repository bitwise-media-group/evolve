// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/plan"
	"github.com/bitwise-media-group/evolve/internal/provider"
)

// formAction is what a key press resolved to on the selection screen.
type formAction int

const (
	actionNone formAction = iota
	actionRun
	actionCancel
)

// The form's three focusable panes.
const (
	paneModels = iota
	paneTriggers
	paneEvals
	paneCount
)

// formModel is the selection screen: a providers/models tree on the left, and
// the right side split lazygit-style into a triggers tree (top) and an evals
// tree (bottom), each plugin → skill → case.
type formModel struct {
	left     tree // providers -> models
	triggers tree // plugin -> skills -> triggers
	evals    tree // plugin -> skills -> evals
	cat      []plan.SkillCatalog
	sels     []provider.Selection
	needs    map[string]map[plan.CaseRef]bool // resolved model key -> case -> needs run
	focus    int
	w, h     int
}

func newForm(
	cat []plan.SkillCatalog, sels []provider.Selection,
	needs map[string]map[plan.CaseRef]bool, notes map[plan.CaseRef]string, evalFilter string,
) formModel {
	st := deriveStates(needs)
	f := formModel{
		left:     buildProviderTree(sels, st),
		triggers: buildTierTree(cat, plan.KindTriggers, st, notes, evalFilter),
		evals:    buildTierTree(cat, plan.KindEvals, st, notes, evalFilter),
		cat:      cat,
		sels:     sels,
		needs:    needs,
	}
	// Start compact: only branches that contain a selected case are open.
	f.left.collapseUnselected()
	f.triggers.collapseUnselected()
	f.evals.collapseUnselected()
	f.resolve() // seed the case panes' displayed state from the initial plan
	return f
}

// formStates holds the form's initial tri-state selection, derived so the run
// reproduces non-TUI mode exactly: a model/case is fully on only when it would
// run for every involved counterpart, partial when it runs for some, off when
// none. Case annotations are the per-case reasons (held separately in notes),
// not a fraction; only the model fraction stays here.
type formStates struct {
	model     map[string]nodeState // model key -> state
	mNote     map[string]string    // grey fraction for partial models
	caseState map[plan.CaseRef]nodeState
}

func deriveStates(needs map[string]map[plan.CaseRef]bool) formStates {
	involvedCases := map[plan.CaseRef]bool{}
	involvedModels := map[string]bool{}
	for mk, cm := range needs {
		for cr, need := range cm {
			if need {
				involvedCases[cr] = true
				involvedModels[mk] = true
			}
		}
	}
	s := formStates{
		model:     map[string]nodeState{},
		mNote:     map[string]string{},
		caseState: map[plan.CaseRef]nodeState{},
	}
	for mk := range needs {
		got := 0
		for cr := range involvedCases {
			if needs[mk][cr] {
				got++
			}
		}
		total := len(involvedCases)
		switch got {
		case 0:
			s.model[mk] = nodeOff
		case total:
			s.model[mk] = nodeOn
		default:
			s.model[mk] = nodePartial
			s.mNote[mk] = fmt.Sprintf("(%d/%d)", got, total)
		}
	}
	for cr := range involvedCases {
		got := 0
		for mk := range involvedModels {
			if needs[mk][cr] {
				got++
			}
		}
		if total := len(involvedModels); got == total {
			s.caseState[cr] = nodeOn
		} else {
			s.caseState[cr] = nodePartial
		}
	}
	return s
}

// buildProviderTree lists every available provider/model; the derived states
// decide which start on/partial/off, so the config/flags (and --new) preselect
// a subset of the full matrix — the same semantics as the case trees.
func buildProviderTree(sels []provider.Selection, st formStates) tree {
	var t tree
	group := map[string]int{}
	for i, sel := range sels {
		name := sel.Provider.Name()
		pidx, ok := group[name]
		if !ok {
			pidx = t.add(treeNode{label: sel.Provider.Display(), parent: -1, expanded: true, selIdx: -1})
			group[name] = pidx
		}
		label := sel.Model.Display
		if label == "" {
			label = sel.Model.ID
		}
		k := sel.Key()
		t.add(treeNode{
			label: label, note: st.mNote[k], depth: 1, parent: pidx, leaf: true,
			state: st.model[k], selIdx: i,
		})
	}
	return t
}

type caseRow struct {
	key   string
	label string
	skip  map[string]bool
}

func skipSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func tierCases(sc plan.SkillCatalog, kind plan.Kind) []caseRow {
	var rows []caseRow
	if kind == plan.KindTriggers {
		for _, tr := range sc.Triggers {
			rows = append(rows, caseRow{key: tr.Query, label: triggerLabel(tr), skip: skipSet(tr.SkipProviders)})
		}
		return rows
	}
	for _, ev := range sc.Evals {
		rows = append(rows, caseRow{key: ev.ID, label: evalLabel(ev), skip: skipSet(ev.SkipProviders)})
	}
	return rows
}

// buildTierTree builds a plugin → skill → case tree for one tier, with each leaf
// in its per-case tri-state and annotated with the reason it is preselected.
// --eval forces non-matching evals off.
func buildTierTree(cat []plan.SkillCatalog, kind plan.Kind, st formStates,
	notes map[plan.CaseRef]string, evalFilter string,
) tree {
	var t tree
	pluginNode := map[string]int{}
	for _, sc := range cat {
		cases := tierCases(sc, kind)
		if len(cases) == 0 {
			continue
		}
		pidx, ok := pluginNode[sc.Plugin]
		if !ok {
			pidx = t.add(treeNode{label: sc.Plugin, parent: -1, expanded: true, selIdx: -1})
			pluginNode[sc.Plugin] = pidx
		}
		sidx := t.add(treeNode{
			label: sc.Skill, depth: 1, parent: pidx, expanded: true,
			skill: sc.Skill, kind: kind, selIdx: -1,
		})
		for _, c := range cases {
			cr := plan.CaseRef{Skill: sc.Skill, Kind: kind, Case: c.key}
			state, note := st.caseState[cr], notes[cr]
			if kind == plan.KindEvals && evalFilter != "" && c.key != evalFilter {
				state, note = nodeOff, ""
			}
			t.add(treeNode{
				label: c.label, note: note, depth: 2, parent: sidx, leaf: true,
				state: state, skill: sc.Skill, kind: kind, caseKey: c.key, skip: c.skip, selIdx: -1,
			})
		}
	}
	return t
}

func (f *formModel) focused() *tree {
	switch f.focus {
	case paneTriggers:
		return &f.triggers
	case paneEvals:
		return &f.evals
	default:
		return &f.left
	}
}

func (f formModel) valid() bool {
	return f.left.anyChecked() && (f.triggers.anyChecked() || f.evals.anyChecked())
}

// update handles one key on the form and reports whether the user chose to run
// or cancel.
func (f formModel) update(key string) (formModel, formAction) {
	t := f.focused()
	switch key {
	case "tab":
		f.focus = (f.focus + 1) % paneCount
	case "shift+tab":
		f.focus = (f.focus + paneCount - 1) % paneCount
	case "1":
		f.focus = paneModels
	case "2":
		f.focus = paneTriggers
	case "3":
		f.focus = paneEvals
	case "up", "k":
		t.move(-1)
	case "down", "j":
		t.move(1)
	case "left", "h":
		t.expand(false)
	case "right", "l":
		t.expand(true)
	case "]":
		t.expandLevel()
	case "[":
		t.collapseLevel()
	case "g", "home":
		t.top()
	case "G", "end":
		t.bottom()
	case " ", "space":
		if i := t.currentNode(); i >= 0 {
			t.toggle(i)
			f.resolve() // re-resolve the plan so the case panes reflect the new selection
		}
	case "enter":
		if i := t.currentNode(); i >= 0 {
			if t.nodes[i].leaf {
				t.toggle(i)
				f.resolve()
			} else {
				t.nodes[i].expanded = !t.nodes[i].expanded
			}
		}
	case "r":
		if f.valid() {
			return f, actionRun
		}
	case "esc", "q", "ctrl+c":
		return f, actionCancel
	}
	return f, actionNone
}

// request turns the current selection into a RunRequest: the enabled models (in
// display order) and a plan.Selection capturing each model's and case's intent
// plus the preselect baseline. The engine resolves it through plan.Build — the
// same resolver this form previews with — so what runs matches what is shown.
func (f formModel) request() RunRequest {
	return RunRequest{Models: f.enabledModels(), Selection: f.selection()}
}

// selection captures the form's current intent as a plan.Selection over the whole
// matrix (every model and case, plus the preselect baseline).
func (f formModel) selection() plan.Selection {
	sel := plan.Selection{
		Models: map[string]plan.State{},
		Cases:  map[plan.CaseRef]plan.State{},
		Needs:  f.needs,
	}
	for _, mn := range f.left.nodes {
		if mn.leaf {
			sel.Models[f.sels[mn.selIdx].Key()] = planState(mn.state)
		}
	}
	for _, t := range []*tree{&f.triggers, &f.evals} {
		for _, cn := range t.nodes {
			if cn.leaf {
				sel.Cases[plan.CaseRef{Skill: cn.skill, Kind: cn.kind, Case: cn.caseKey}] = planState(cn.state)
			}
		}
	}
	return sel
}

// enabledModels is the selections whose model leaf is not off, in display order.
func (f formModel) enabledModels() []provider.Selection {
	var out []provider.Selection
	for _, mn := range f.left.nodes {
		if mn.leaf && planState(mn.state) != plan.Off {
			out = append(out, f.sels[mn.selIdx])
		}
	}
	return out
}

// resolve rebuilds the live plan from the current selection and refreshes the
// case panes' displayed checkboxes from it. Disabling a model immediately
// unchecks a case only that model would have run, so the form shows exactly what
// will run before the user submits — the engine owns the resolution, the form
// just reflects it.
func (f *formModel) resolve() {
	disp := caseDisplayFromPlan(plan.Build(f.cat, f.enabledModels(), f.selection(), plan.PriorMetrics{}))
	for _, t := range []*tree{&f.triggers, &f.evals} {
		t.display = map[int]nodeState{}
		for i, cn := range t.nodes {
			if cn.leaf {
				t.display[i] = disp[plan.CaseRef{Skill: cn.skill, Kind: cn.kind, Case: cn.caseKey}]
			}
		}
	}
}

// caseDisplayFromPlan reduces a resolved plan to a per-case checkbox state: a case
// is on when every enabled model that has it will run it, off when none will, and
// partial in between. A case absent from the plan (no enabled model has it) is off.
func caseDisplayFromPlan(p plan.Plan) map[plan.CaseRef]nodeState {
	type agg struct{ queued, total int }
	tally := map[plan.CaseRef]*agg{}
	for _, pl := range p.Plugins {
		for _, sk := range pl.Skills {
			for _, mdl := range sk.Models {
				for _, u := range mdl.Units {
					for _, c := range u.Cases {
						cr := plan.CaseRef{Skill: sk.Skill, Kind: c.Kind, Case: c.Label}
						a := tally[cr]
						if a == nil {
							a = &agg{}
							tally[cr] = a
						}
						a.total++
						if c.Queued {
							a.queued++
						}
					}
				}
			}
		}
	}
	out := make(map[plan.CaseRef]nodeState, len(tally))
	for cr, a := range tally {
		switch a.queued {
		case 0:
			out[cr] = nodeOff
		case a.total:
			out[cr] = nodeOn
		default:
			out[cr] = nodePartial
		}
	}
	return out
}

// planState maps a form node's tri-state to the planner's.
func planState(s nodeState) plan.State {
	switch s {
	case nodeOn:
		return plan.On
	case nodePartial:
		return plan.Partial
	default:
		return plan.Off
	}
}

// view renders the providers pane beside the stacked triggers/evals panes, with
// a button/hint footer.
func (f formModel) view() string {
	const footerH = 4
	paneH := max(f.h-footerH, 6)
	leftW := max(f.w/3, 16)
	rightW := max(f.w-leftW, 16)
	topH := paneH / 2
	botH := paneH - topH

	mc, mt := f.left.counts()
	tc, tt := f.triggers.counts()
	ec, et := f.evals.counts()

	left := panel(1, "Providers / Models", countLabel(mc, mt), "",
		renderTree(&f.left, f.focus == paneModels, panelContentWidth(leftW), paneH-2),
		f.focus == paneModels, leftW, paneH)
	trig := panel(2, "Triggers", countLabel(tc, tt), "",
		renderTree(&f.triggers, f.focus == paneTriggers, panelContentWidth(rightW), topH-2),
		f.focus == paneTriggers, rightW, topH)
	eval := panel(3, "Evaluations", countLabel(ec, et), "",
		renderTree(&f.evals, f.focus == paneEvals, panelContentWidth(rightW), botH-2),
		f.focus == paneEvals, rightW, botH)
	right := lipgloss.JoinVertical(lipgloss.Left, trig, eval)
	panes := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	runBtn := buttonStyle.Render("r  RUN")
	if f.valid() {
		runBtn = buttonActive.Render("r  RUN")
	}
	cancel := buttonStyle.Render("esc  CANCEL")
	buttons := lipgloss.JoinHorizontal(lipgloss.Top, cancel, "  ", runBtn)
	hint := footerHint.Render("1/2/3 pane · ↑↓/jk move · ←→/hl fold · [ ] fold level · space toggle · g/G ends")
	footer := lipgloss.JoinVertical(lipgloss.Left, buttons, hint)

	return lipgloss.JoinVertical(lipgloss.Left, panes, footer)
}

// countLabel renders the "checked of total" tag for a pane's bottom border.
func countLabel(checked, total int) string {
	return fmt.Sprintf("%d of %d", checked, total)
}

// renderTree draws the visible rows, scrolled to keep the cursor on screen.
func renderTree(t *tree, focused bool, w, h int) string {
	vis := t.visible()
	if t.cursor >= len(vis) {
		t.cursor = len(vis) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	rows := max(h, 1)
	start := 0
	if t.cursor >= rows {
		start = t.cursor - rows + 1
	}
	end := min(start+rows, len(vis))

	var b strings.Builder
	for pos := start; pos < end; pos++ {
		i := vis[pos]
		n := t.nodes[i]
		box := checkbox(t, i)
		arrow := "  "
		if !n.leaf && len(n.children) > 0 {
			if n.expanded {
				arrow = "▾ "
			} else {
				arrow = "▸ "
			}
		}
		line := strings.Repeat("  ", n.depth) + arrow + box + " " + n.label
		if n.leaf && n.state != nodeOff && n.note != "" {
			line += " " + mutedStyle.Render(n.note)
		}
		line = clip(line, w-2) // leave room for the 2-col cursor marker
		if pos == t.cursor && focused {
			line = selectedStyle.Render("› ") + line
		} else {
			line = "  " + line
		}
		b.WriteString(line)
		if pos < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func checkbox(t *tree, i int) string {
	switch t.displayState(i) {
	case nodeOn:
		return passStyle.Render("[x]")
	case nodePartial:
		return errStyle.Render("[~]")
	default:
		return mutedStyle.Render("[ ]")
	}
}

func triggerLabel(tr evalspec.Trigger) string {
	mark := "−"
	if tr.ShouldTrigger {
		mark = "+"
	}
	return fmt.Sprintf("%s %s", mark, truncate(tr.Query, 70))
}

func evalLabel(ev evalspec.Eval) string {
	if ev.Name != "" {
		return fmt.Sprintf("%s (%s)", ev.Name, ev.ID)
	}
	return ev.ID
}
