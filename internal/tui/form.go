// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/run"
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
	sels     []provider.Selection
	needs    map[string]map[run.CaseRef]bool // resolved model key -> case -> needs run
	focus    int
	w, h     int
}

func newForm(
	cat []run.SkillCatalog, sels []provider.Selection,
	needs map[string]map[run.CaseRef]bool, notes map[run.CaseRef]string, evalFilter string,
) formModel {
	st := deriveStates(needs)
	f := formModel{
		left:     buildProviderTree(sels, st),
		triggers: buildTierTree(cat, run.KindTriggers, st, notes, evalFilter),
		evals:    buildTierTree(cat, run.KindEvals, st, notes, evalFilter),
		sels:     sels,
		needs:    needs,
	}
	// Start compact: only branches that contain a selected case are open.
	f.left.collapseUnselected()
	f.triggers.collapseUnselected()
	f.evals.collapseUnselected()
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
	caseState map[run.CaseRef]nodeState
}

func deriveStates(needs map[string]map[run.CaseRef]bool) formStates {
	involvedCases := map[run.CaseRef]bool{}
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
		caseState: map[run.CaseRef]nodeState{},
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

func tierCases(sc run.SkillCatalog, kind run.Kind) []caseRow {
	var rows []caseRow
	if kind == run.KindTriggers {
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
func buildTierTree(cat []run.SkillCatalog, kind run.Kind, st formStates,
	notes map[run.CaseRef]string, evalFilter string) tree {

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
			cr := run.CaseRef{Skill: sc.Skill, Kind: kind, Case: c.key}
			state, note := st.caseState[cr], notes[cr]
			if kind == run.KindEvals && evalFilter != "" && c.key != evalFilter {
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
		}
	case "enter":
		if i := t.currentNode(); i >= 0 {
			if t.nodes[i].leaf {
				t.toggle(i)
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

// request turns the current selection into a RunRequest: a per-model filter so
// each model runs exactly the cases it should. The run rule per (model m, case
// c): both must be selected (on/partial) and applicable, and then it runs if
// the model is fully on (runs all its selected cases), or the case is fully on
// (runs for all selected models), or — both partial — the per-case matrix says m
// needs c. That reproduces non-TUI mode while letting the user widen it.
func (f formModel) request() RunRequest {
	var models []provider.Selection
	filters := map[string]*run.Filter{}
	for _, mn := range f.left.nodes {
		if !mn.leaf || mn.state == nodeOff {
			continue
		}
		sel := f.sels[mn.selIdx]
		ff := &run.Filter{
			Skills:   map[string]bool{},
			Triggers: map[string]map[string]bool{},
			Evals:    map[string]map[string]bool{},
		}
		f.collectInto(ff, mn.state, sel, f.triggers, run.KindTriggers)
		f.collectInto(ff, mn.state, sel, f.evals, run.KindEvals)
		if len(ff.Skills) > 0 {
			models = append(models, sel)
			filters[sel.Key()] = ff
		}
	}
	return RunRequest{Models: models, Filters: filters}
}

// collectInto adds the cases of one tier that this model should run into ff.
func (f formModel) collectInto(ff *run.Filter, mState nodeState, sel provider.Selection, src tree, kind run.Kind) {
	dst := ff.Triggers
	if kind == run.KindEvals {
		dst = ff.Evals
	}
	mk, provName := sel.Key(), sel.Provider.Name()
	for _, cn := range src.nodes {
		if !cn.leaf {
			continue
		}
		// Seed an (empty, non-nil) per-skill set so an unselected tier reads as
		// "none included", not "unrestricted".
		if dst[cn.skill] == nil {
			dst[cn.skill] = map[string]bool{}
		}
		if cn.state == nodeOff || cn.skip[provName] {
			continue
		}
		if mState == nodeOn || cn.state == nodeOn || f.needs[mk][run.CaseRef{Skill: cn.skill, Kind: kind, Case: cn.caseKey}] {
			dst[cn.skill][cn.caseKey] = true
			ff.Skills[cn.skill] = true
		}
	}
}

// view renders the providers pane beside the stacked triggers/evals panes, with
// a button/hint footer.
func (f formModel) view() string {
	const footerH = 4
	paneH := f.h - footerH
	if paneH < 6 {
		paneH = 6
	}
	leftW := f.w / 3
	if leftW < 16 {
		leftW = 16
	}
	rightW := f.w - leftW
	if rightW < 16 {
		rightW = 16
	}
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
	rows := h
	if rows < 1 {
		rows = 1
	}
	start := 0
	if t.cursor >= rows {
		start = t.cursor - rows + 1
	}
	end := start + rows
	if end > len(vis) {
		end = len(vis)
	}

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
	st := t.checkState(i)
	if t.nodes[i].leaf {
		st = t.nodes[i].state
	}
	switch st {
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
