// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/plan"
)

// formAction is what a key press resolved to on the selection screen.
type formAction int

const (
	actionNone formAction = iota
	actionRun
	actionCancel
)

// The form's focusable regions: the four selection panes plus the button row,
// reached by tab from the tree. The 1/2/3/4 jump keys map to the first four, so
// paneButtons must stay last (before paneCount).
const (
	paneFilters = iota
	paneHarness
	paneModels
	paneTree
	paneButtons
	paneCount
)

// The two buttons in the paneButtons region.
const (
	btnCancel = iota
	btnRun
)

// The button labels, shared by buttons() and layout() so the rendered row and
// the click targets cannot drift.
const (
	btnCancelLabel = "CANCEL [esc]"
	btnRunLabel    = "RUN [r]"
)

// Case-marker and selection glyphs. Markers identify the tier; the selection
// glyph shows the resolved state the Session computes (force on/off, or auto
// queued for all / some / none of a node's applicable enabled models).
const (
	markTrigger = "⌖"
	markEval    = "⚙"

	glyphForceOn  = "☑"
	glyphForceOff = "☐"
	glyphAutoAll  = "◉"
	glyphPartial  = "◷"
	glyphAutoNone = "○"
)

// formModel is the selection screen: a left column stacking the filter,
// harness, and model panes, beside a plugin → skill → case tree. All selection
// state lives in the plan.Session; the form navigates and routes key presses to
// the Session's receivers, then renders each pane's glyphs from it.
type formModel struct {
	session *plan.Session
	cat     []plan.SkillCatalog

	filters  list
	harness  list
	models   list
	tree     tree
	focus    int
	btnFocus int // which button is selected while focus == paneButtons
	w, h     int
}

func newForm(session *plan.Session, cat []plan.SkillCatalog, evalFilter string) formModel {
	f := formModel{
		session: session,
		cat:     cat,
		filters: list{items: []listItem{
			{label: "new", id: "new"},
			{label: "modified", id: "modified"},
			{label: "failed", id: "failed"},
		}},
		tree: buildCaseTree(cat),
	}
	for _, h := range session.Harnesses() {
		f.harness.items = append(f.harness.items, listItem{label: h.Harness.Name(), id: h.Harness.ID()})
	}
	// Models arrive provider-grouped in registry order; emit a header row at each
	// provider boundary so a whole vendor can be toggled at once.
	lastProv := ""
	for _, m := range session.Models() {
		if m.ProviderID != lastProv {
			lastProv = m.ProviderID
			name := m.ProviderID
			if p, ok := model.ProviderByID(m.ProviderID); ok {
				name = p.Name
			}
			f.models.items = append(f.models.items,
				listItem{label: name, id: "provider:" + m.ProviderID, group: m.ProviderID, header: true})
		}
		f.models.items = append(f.models.items, listItem{label: m.Name, id: m.Key()})
	}
	// --eval forces every non-matching eval off so the run is scoped to it.
	if evalFilter != "" {
		for _, n := range f.tree.nodes {
			if n.leaf && n.kind == plan.KindEvals && n.caseKey != evalFilter {
				session.SetCases([]plan.CaseRef{{Skill: n.skill, Kind: plan.KindEvals, Case: n.caseKey}}, plan.Off)
			}
		}
	}
	// Open branches that have a preselected case; keep the rest compact.
	f.tree.expandWhere(func(cr plan.CaseRef) bool {
		switch session.NodeSel([]plan.CaseRef{cr}) {
		case plan.SelForceOn, plan.SelAutoAll, plan.SelAutoPartial:
			return true
		default:
			return false
		}
	})
	return f
}

// buildCaseTree builds the plugin → skill → case tree: under each skill the
// trigger cases come first (target glyph), then the eval cases (gear glyph).
func buildCaseTree(cat []plan.SkillCatalog) tree {
	var t tree
	pluginNode := map[string]int{}
	for _, sc := range cat {
		if len(sc.Triggers) == 0 && len(sc.Evals) == 0 {
			continue
		}
		pidx, ok := pluginNode[sc.Plugin]
		if !ok {
			pidx = t.add(treeNode{label: sc.Plugin, parent: -1, expanded: true})
			pluginNode[sc.Plugin] = pidx
		}
		sidx := t.add(treeNode{label: sc.Skill, depth: 1, parent: pidx, expanded: true, skill: sc.Skill})
		for _, tr := range sc.Triggers {
			t.add(treeNode{
				label: triggerLabel(tr), depth: 2, parent: sidx, leaf: true,
				skill: sc.Skill, kind: plan.KindTriggers, caseKey: tr.Query, hasKind: true,
			})
		}
		for _, ev := range sc.Evals {
			t.add(treeNode{
				label: evalLabel(ev), depth: 2, parent: sidx, leaf: true,
				skill: sc.Skill, kind: plan.KindEvals, caseKey: ev.ID, hasKind: true,
			})
		}
	}
	return t
}

func (f formModel) valid() bool { return f.session.AnyQueued() }

// update handles one key on the form and reports whether the user chose to run
// or cancel. Toggles route to the Session; the next render reflects the result.
func (f formModel) update(key string) (formModel, formAction) {
	switch key {
	case "tab":
		f.cycleFocus(1)
	case "shift+tab":
		f.cycleFocus(-1)
	case "1":
		f.focus = paneFilters
	case "2":
		f.focus = paneHarness
	case "3":
		f.focus = paneModels
	case "4":
		f.focus = paneTree
	case "up", "k":
		f.moveCursor(-1)
	case "down", "j":
		f.moveCursor(1)
	case "left", "h":
		f.horizontal(false)
	case "right", "l":
		f.horizontal(true)
	case "]":
		if f.focus == paneTree {
			f.tree.expandLevel()
		}
	case "[":
		if f.focus == paneTree {
			f.tree.collapseLevel()
		}
	case "g", "home":
		f.toEnd(true)
	case "G", "end":
		f.toEnd(false)
	case " ", "space", "enter":
		if f.focus == paneButtons {
			return f, f.pressButton()
		}
		f.toggle()
	case "r":
		if f.valid() {
			return f, actionRun
		}
	case "esc", "q", "ctrl+c":
		return f, actionCancel
	}
	return f, actionNone
}

// cycleFocus advances the focused region by delta (wrapping), defaulting the
// button row to RUN whenever it gains focus.
func (f *formModel) cycleFocus(delta int) {
	f.focus = (f.focus + delta + paneCount) % paneCount
	if f.focus == paneButtons {
		f.btnFocus = btnRun
	}
}

// horizontal handles left/right in the focused region: folding in the tree,
// selecting between the two buttons in the button row.
func (f *formModel) horizontal(open bool) {
	switch f.focus {
	case paneTree:
		f.tree.expand(open)
	case paneButtons:
		f.btnFocus = btnCancel
		if open {
			f.btnFocus = btnRun
		}
	}
}

// pressButton resolves enter/space on the focused button. RUN is inert until a
// run is queued.
func (f formModel) pressButton() formAction {
	if f.btnFocus == btnCancel {
		return actionCancel
	}
	if f.valid() {
		return actionRun
	}
	return actionNone
}

// handleMouse routes a mouse event on the form. A left click focuses the pane
// under the cursor and acts on the row or button it hit; the wheel moves the
// pane's cursor without changing focus (the form's windows are
// cursor-anchored, so the cursor is the scroll position).
func (f formModel) handleMouse(msg tea.MouseMsg) (formModel, formAction) {
	switch m := msg.(type) {
	case tea.MouseClickMsg:
		if m.Button == tea.MouseLeft {
			return f.click(m.X, m.Y)
		}
	case tea.MouseWheelMsg:
		switch m.Button {
		case tea.MouseWheelUp:
			f.wheelAt(m.X, m.Y, -1)
		case tea.MouseWheelDown:
			f.wheelAt(m.X, m.Y, 1)
		}
	}
	return f, actionNone
}

// click focuses what was hit and applies it: buttons activate, list rows
// toggle, tree parents fold, and a repeat click on the leaf already under the
// cursor cycles its selection.
func (f formModel) click(x, y int) (formModel, formAction) {
	l := f.layout()
	switch {
	case l.cancelBtn.contains(x, y):
		f.focus, f.btnFocus = paneButtons, btnCancel
		return f, actionCancel
	case l.runBtn.contains(x, y):
		f.focus, f.btnFocus = paneButtons, btnRun
		if f.valid() {
			return f, actionRun
		}
	case l.filters.contains(x, y):
		f.clickList(paneFilters, &f.filters, l.filters, y)
	case l.harness.contains(x, y):
		f.clickList(paneHarness, &f.harness, l.harness, y)
	case l.models.contains(x, y):
		f.clickList(paneModels, &f.models, l.models, y)
	case l.tree.contains(x, y):
		f.clickTree(l.tree, y)
	}
	return f, actionNone
}

// clickList focuses a list pane and, when the click lands on a row, moves the
// cursor there and toggles it — checkbox rows are unambiguous click targets,
// and toggle routes to the same Session receivers the space key uses.
func (f *formModel) clickList(region int, lst *list, r rect, y int) {
	f.focus = region
	c := contentRect(r)
	if y < c.y0 || y >= c.y1 {
		return
	}
	idx := topWindowStart(lst.cursor, c.h()) + (y - c.y0)
	if idx >= len(lst.items) {
		return
	}
	lst.setCursor(idx)
	f.toggle()
}

// clickTree focuses the tree and applies the clicked row: a parent folds or
// unfolds, a leaf takes the cursor, and only a repeat click on the leaf
// already under the cursor cycles its tri-state — cycling on first contact
// would flip cases while the user is merely selecting.
func (f *formModel) clickTree(r rect, y int) {
	wasFocused := f.focus == paneTree
	f.focus = paneTree
	c := contentRect(r)
	if y < c.y0 || y >= c.y1 {
		return
	}
	vis := f.tree.visible()
	if len(vis) == 0 {
		return
	}
	// renderTree windows off the clamped cursor, so the hit-test must too.
	cursor := clampInt(f.tree.cursor, 0, len(vis)-1)
	pos := topWindowStart(cursor, c.h()) + (y - c.y0)
	if pos >= len(vis) {
		return
	}
	i := vis[pos]
	f.tree.cursor = pos
	if !f.tree.nodes[i].leaf && len(f.tree.nodes[i].children) > 0 {
		f.tree.nodes[i].expanded = !f.tree.nodes[i].expanded
		return
	}
	if wasFocused && cursor == pos {
		f.toggle()
	}
}

// wheelAt moves the cursor of the pane under the mouse without changing focus.
func (f *formModel) wheelAt(x, y, delta int) {
	l := f.layout()
	switch {
	case l.filters.contains(x, y):
		f.moveIn(paneFilters, delta)
	case l.harness.contains(x, y):
		f.moveIn(paneHarness, delta)
	case l.models.contains(x, y):
		f.moveIn(paneModels, delta)
	case l.tree.contains(x, y):
		f.moveIn(paneTree, delta)
	}
}

// moveCursor advances the focused region's cursor.
func (f *formModel) moveCursor(delta int) { f.moveIn(f.focus, delta) }

// moveIn advances one region's cursor, focused or not — the wheel scrolls the
// pane under the mouse without moving focus.
func (f *formModel) moveIn(region, delta int) {
	switch region {
	case paneFilters:
		f.filters.move(delta)
	case paneHarness:
		f.harness.move(delta)
	case paneModels:
		f.models.move(delta)
	case paneTree:
		f.tree.move(delta)
	}
}

// toEnd jumps the focused region's cursor to the top (start=true) or bottom.
func (f *formModel) toEnd(start bool) {
	jump := func(l *list) {
		if start {
			l.top()
		} else {
			l.bottom()
		}
	}
	switch f.focus {
	case paneFilters:
		jump(&f.filters)
	case paneHarness:
		jump(&f.harness)
	case paneModels:
		jump(&f.models)
	case paneTree:
		if start {
			f.tree.top()
		} else {
			f.tree.bottom()
		}
	}
}

// toggle applies the toggle key to the focused region's current selection.
func (f *formModel) toggle() {
	switch f.focus {
	case paneFilters:
		if it, ok := f.filters.current(); ok {
			cur := f.session.FilterState()
			switch it.id {
			case "new":
				f.session.SetNewFilter(!cur.New)
			case "modified":
				f.session.SetModifiedFilter(!cur.Modified)
			case "failed":
				f.session.SetFailedFilter(!cur.Failed)
			}
		}
	case paneHarness:
		if it, ok := f.harness.current(); ok && f.harnessAvailable(it.id) {
			f.session.EnableHarness(it.id, !f.session.HarnessEnabled(it.id))
		}
	case paneModels:
		if it, ok := f.models.current(); ok {
			if it.header {
				f.toggleProvider(it.group)
			} else if f.modelRunnable(it.id) {
				f.session.EnableModel(it.id, !f.session.ModelEnabled(it.id))
			}
		}
	case paneTree:
		if i := f.tree.currentNode(); i >= 0 {
			f.cycleNode(f.tree.caseLeaves(i))
		}
	}
}

// cycleNode advances a node through auto → off → on → auto (the auto slot is
// skipped when nothing under the node is queued).
func (f *formModel) cycleNode(refs []plan.CaseRef) {
	switch f.session.NodeSel(refs) {
	case plan.SelForceOff:
		f.session.SetCases(refs, plan.On)
	case plan.SelForceOn:
		if f.session.AutoAvailable(refs) {
			f.session.SetCases(refs, plan.Partial)
		} else {
			f.session.SetCases(refs, plan.Off)
		}
	default: // any auto state
		f.session.SetCases(refs, plan.Off)
	}
}

func (f formModel) harnessAvailable(id string) bool {
	for _, h := range f.session.Harnesses() {
		if h.Harness.ID() == id {
			return h.Available
		}
	}
	return false
}

func (f formModel) modelRunnable(key string) bool {
	for _, m := range f.session.Models() {
		if m.Key() == key {
			return f.session.ModelRunnable(m)
		}
	}
	return false
}

// toggleProvider enables every runnable model under a provider, or disables them
// all when they are already enabled — the group analogue of EnableModel.
func (f *formModel) toggleProvider(provID string) {
	on, total := f.providerState(provID)
	enable := on < total
	for _, m := range f.session.Models() {
		if m.ProviderID == provID && f.session.ModelRunnable(m) {
			f.session.EnableModel(m.Key(), enable)
		}
	}
}

// providerState counts how many of a provider's runnable models are enabled, for
// the header row's tri-state box.
func (f formModel) providerState(provID string) (on, total int) {
	for _, m := range f.session.Models() {
		if m.ProviderID == provID && f.session.ModelRunnable(m) {
			total++
			if f.session.ModelEnabled(m.Key()) {
				on++
			}
		}
	}
	return on, total
}

// request turns the current Session state into a RunRequest: the enabled
// (model, harness) selections and the resolved plan.Selection. The engine and
// dashboard re-Build from these — the same inputs the Session's Plan() uses — so
// what runs matches what the form showed.
func (f formModel) request() RunRequest {
	return RunRequest{Models: f.session.EnabledSelections(), Selection: f.session.Selection()}
}

// view renders the filter/harness/model column beside the case tree, with a
// button/hint footer.
func (f formModel) view() string {
	// Every pane renders at the rect layout() reports, so mouse hit-testing
	// shares this exact geometry.
	l := f.layout()
	leftW := l.filters.w()
	rightW := l.tree.w()

	// Accents mirror the dashboard horizontally: its right column becomes our left
	// column (green/teal/orange top-to-bottom) and its single tall left pane becomes
	// our tall right pane (pink). The title stays bright and only the border dims
	// when a pane is unfocused — same as the dashboard.
	cw := panelContentWidth(leftW)
	filters := panel(1, "Filters", "", "",
		f.renderFilters(cw, l.filters.h()-2), f.focus == paneFilters, leftW, l.filters.h(), accentRollup)
	harness := panel(2, "Harnesses", "", "",
		f.renderHarness(cw, l.harness.h()-2), f.focus == paneHarness, leftW, l.harness.h(), accentRuns)
	models := panel(3, "Models", f.modelCount(), "",
		f.renderModels(cw, l.models.h()-2), f.focus == paneModels, leftW, l.models.h(), accentDetails)
	left := lipgloss.JoinVertical(lipgloss.Left, filters, harness, models)

	// A glyph legend sits directly under the tree pane: one row when it fits the pane
	// width, two when it does not. On a short terminal it is dropped so the tree keeps
	// usable height. The height is recomputed every render, so resize re-flows it.
	tree := panel(4, "Plugins / Skills / Cases", "", "",
		f.renderTree(panelContentWidth(rightW), l.tree.h()-2), f.focus == paneTree, rightW, l.tree.h(), accentExec)
	right := tree
	if l.paneH >= 13 {
		legendBody, legendH := f.legend(rightW)
		legend := panel(0, "Legend", "", "", legendBody, false, rightW, legendH, accentExec)
		right = lipgloss.JoinVertical(lipgloss.Left, tree, legend)
	}
	panes := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	footer := lipgloss.JoinVertical(lipgloss.Left, f.buttons(), f.hint())

	// Leading space insets the wordmark one column to match the dashboard's title bar.
	return lipgloss.JoinVertical(lipgloss.Left, " "+evolveTitle(), "", panes, footer)
}

// formLayout reports where each form pane and button lands on screen as outer
// rects in terminal cells. view() renders from it and handleMouse hit-tests
// against it, so the two can never disagree about the geometry.
type formLayout struct {
	filters, harness, models, tree rect
	cancelBtn, runBtn              rect
	paneH                          int
}

// layout computes the current frame's geometry from the window size: two
// chrome rows (the wordmark and the blank line beneath it), then the left
// column stacking filters/harnesses/models beside the tree (the legend under
// it is inert, so it carries no rect), then the right-aligned button row.
// paneH takes the header (2 rows) and footer (the bordered 3-row buttons plus
// the hint line) off the window height.
func (f formModel) layout() formLayout {
	const bodyTop = 2
	paneH := max(f.h-6, 8)
	leftW := max(f.w/3, 20)
	rightW := max(f.w-leftW, 20)

	filtersH := 5
	harnessH := min(len(f.harness.items)+2, max(paneH/3, 5))
	modelsH := max(paneH-filtersH-harnessH, 4)

	treeH := paneH
	if paneH >= 13 {
		_, legendH := f.legend(rightW)
		treeH = paneH - legendH
	}

	l := formLayout{
		paneH:   paneH,
		filters: rect{0, bodyTop, leftW, bodyTop + filtersH},
		harness: rect{0, bodyTop + filtersH, leftW, bodyTop + filtersH + harnessH},
		models:  rect{0, bodyTop + filtersH + harnessH, leftW, bodyTop + filtersH + harnessH + modelsH},
		tree:    rect{leftW, bodyTop, leftW + rightW, bodyTop + treeH},
	}

	// The buttons sit under the taller column: the right one is always paneH
	// tall (tree plus legend), but on a tiny window the left column's height
	// floors can push it past paneH, and JoinHorizontal bottoms the pane block
	// out at the taller of the two.
	btnTop := bodyTop + max(filtersH+harnessH+modelsH, paneH)
	cancelW := lipgloss.Width(buttonStyle.Render(btnCancelLabel))
	runW := lipgloss.Width(buttonStyle.Render(btnRunLabel))
	x0 := max(max(f.w, 1)-(cancelW+2+runW), 0) // PlaceHorizontal's right alignment
	l.cancelBtn = rect{x0, btnTop, x0 + cancelW, btnTop + 3}
	l.runBtn = rect{x0 + cancelW + 2, btnTop, x0 + cancelW + 2 + runW, btnTop + 3}
	return l
}

// buttons renders the right-aligned CANCEL / RUN pair. The focused button takes
// the blue fill; RUN tints green when a run is queued and greys out when nothing
// is, so it reads as disabled even while focused.
func (f formModel) buttons() string {
	cancelBtn := buttonStyle.Render(btnCancelLabel)
	if f.focus == paneButtons && f.btnFocus == btnCancel {
		cancelBtn = buttonActive.Render(btnCancelLabel)
	}

	runBtn := buttonStyle.Render(btnRunLabel)
	switch {
	case f.valid() && f.focus == paneButtons && f.btnFocus == btnRun:
		runBtn = buttonActive.Render(btnRunLabel)
	case f.valid():
		runBtn = buttonReady.Render(btnRunLabel)
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, cancelBtn, "  ", runBtn)
	return lipgloss.PlaceHorizontal(max(f.w, 1), lipgloss.Right, row)
}

// hint is the footer key legend, clipped to the window width.
func (f formModel) hint() string {
	return footerHint.Render(clip(
		"[tab] pane · [↑↓]/[jk] move · [←→]/[hl] fold · [space] toggle · [g]/[G] ends · [r] run · [esc] cancel",
		max(f.w, 1)))
}

func (f formModel) modelCount() string {
	on := 0
	for _, m := range f.session.Models() {
		if f.session.ModelEnabled(m.Key()) {
			on++
		}
	}
	return countLabel(on, len(f.session.Models()))
}

// renderFilters draws the new/modified/failed checkboxes.
func (f formModel) renderFilters(w, h int) string {
	cur := f.session.FilterState()
	on := map[string]bool{"new": cur.New, "modified": cur.Modified, "failed": cur.Failed}
	return renderRows(f.filters.items, f.filters.cursor, f.focus == paneFilters, w, h,
		func(it listItem) string {
			return checkGlyph(on[it.id]) + " " + it.label
		})
}

// renderHarness draws the harness rows, greying any whose CLI is off PATH.
func (f formModel) renderHarness(w, h int) string {
	return renderRows(f.harness.items, f.harness.cursor, f.focus == paneHarness, w, h,
		func(it listItem) string {
			if !f.harnessAvailable(it.id) {
				return errStyle.Render("[!]") + " " + mutedStyle.Render(it.label+" (n/a)")
			}
			return checkGlyph(f.session.HarnessEnabled(it.id)) + " " + it.label
		})
}

// renderModels draws the model rows under per-provider header rows: a header
// carries a tri-state box for its whole vendor; model rows are indented beneath
// it and greyed when not runnable under the enabled harnesses.
func (f formModel) renderModels(w, h int) string {
	return renderRows(f.models.items, f.models.cursor, f.focus == paneModels, w, h,
		func(it listItem) string {
			if it.header {
				on, total := f.providerState(it.group)
				return providerGlyph(on, total) + " " + headerDetailsStyle.Render(it.label)
			}
			if !f.modelRunnable(it.id) {
				return "  " + mutedStyle.Render("[·] "+it.label+" (uns.)")
			}
			return "  " + checkGlyph(f.session.ModelEnabled(it.id)) + " " + it.label
		})
}

// providerGlyph renders a provider header's tri-state box: all runnable models
// enabled, some, or none.
func providerGlyph(on, total int) string {
	switch {
	case total > 0 && on == total:
		return passStyle.Render("[x]")
	case on > 0:
		return errStyle.Render("[~]")
	default:
		return mutedStyle.Render("[ ]")
	}
}

// renderRows draws a flat list with a cursor marker, scrolled to keep the cursor
// visible, each row rendered by line.
func renderRows(items []listItem, cursor int, focused bool, w, h int, line func(listItem) string) string {
	rows := max(h, 1)
	start := topWindowStart(cursor, rows)
	end := min(start+rows, len(items))
	var b strings.Builder
	for i := start; i < end; i++ {
		row := clip(line(items[i]), w-2)
		if i == cursor && focused {
			row = selectedStyle.Render("› ") + row
		} else {
			row = "  " + row
		}
		b.WriteString(row)
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// renderTree draws the visible plugin/skill/case rows, computing each row's
// glyph from the Session, scrolled to keep the cursor on screen.
func (f formModel) renderTree(w, h int) string {
	vis := f.tree.visible()
	if f.tree.cursor >= len(vis) {
		f.tree.cursor = len(vis) - 1
	}
	if f.tree.cursor < 0 {
		f.tree.cursor = 0
	}
	rows := max(h, 1)
	start := topWindowStart(f.tree.cursor, rows)
	end := min(start+rows, len(vis))

	var b strings.Builder
	for pos := start; pos < end; pos++ {
		i := vis[pos]
		n := f.tree.nodes[i]
		arrow := "  "
		if !n.leaf && len(n.children) > 0 {
			if n.expanded {
				arrow = "▾ "
			} else {
				arrow = "▸ "
			}
		}
		glyph := selGlyph(f.session.NodeSel(f.tree.caseLeaves(i)))
		marker := ""
		if n.leaf {
			marker = caseMarker(n.kind) + " "
		}
		row := strings.Repeat("  ", n.depth) + arrow + glyph + " " + marker + n.label
		row = clip(row, w-2)
		if pos == f.tree.cursor && f.focus == paneTree {
			row = selectedStyle.Render("› ") + row
		} else {
			row = "  " + row
		}
		b.WriteString(row)
		if pos < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// legend builds the glyph-legend body for a pane of outer width w, returning the
// content and the panel height it needs: a single content row (height 3) when all
// seven entries fit the pane width, two rows (height 4) otherwise.
func (f formModel) legend(w int) (body string, h int) {
	items := []string{
		legendItem(caseMarker(plan.KindTriggers), "trigger"),
		legendItem(caseMarker(plan.KindEvals), "eval"),
		legendItem(selGlyph(plan.SelForceOn), "forced on"),
		legendItem(selGlyph(plan.SelForceOff), "forced off"),
		legendItem(selGlyph(plan.SelAutoAll), "auto (all run)"),
		legendItem(selGlyph(plan.SelAutoPartial), "auto (some run)"),
		legendItem(selGlyph(plan.SelAutoNone), "auto (none run)"),
	}
	if one := strings.Join(items, "   "); lipgloss.Width(one) <= panelContentWidth(w) {
		return one, 3
	}
	return strings.Join(items[:4], "   ") + "\n" + strings.Join(items[4:], "   "), 4
}

// legendItem pairs a (pre-styled) glyph with a muted label.
func legendItem(glyph, label string) string {
	return glyph + " " + mutedStyle.Render(label)
}

// checkGlyph renders an on/off checkbox.
func checkGlyph(on bool) string {
	if on {
		return passStyle.Render("[x]")
	}
	return mutedStyle.Render("[ ]")
}

// selGlyph maps a node's resolved selection to its rendered glyph.
func selGlyph(s plan.NodeSel) string {
	switch s {
	case plan.SelForceOn:
		return passStyle.Render(glyphForceOn)
	case plan.SelForceOff:
		return mutedStyle.Render(glyphForceOff)
	case plan.SelAutoAll:
		return runStyle.Render(glyphAutoAll)
	case plan.SelAutoPartial:
		return errStyle.Render(glyphPartial)
	default:
		return mutedStyle.Render(glyphAutoNone)
	}
}

func caseMarker(kind plan.Kind) string {
	if kind == plan.KindEvals {
		return mutedStyle.Render(markEval)
	}
	return mutedStyle.Render(markTrigger)
}

// countLabel renders the "checked of total" tag for a pane's bottom border.
func countLabel(checked, total int) string {
	return fmt.Sprintf("%d of %d", checked, total)
}

func triggerLabel(tr evalspec.Trigger) string {
	mark := "−"
	if tr.ShouldTrigger {
		mark = "+"
	}
	// renderTree clips each row to the live pane width, so the full query is kept
	// here and only ellipsized when it actually overflows (re-extending on resize).
	return fmt.Sprintf("%s %s", mark, tr.Query)
}

func evalLabel(ev evalspec.Eval) string {
	if ev.Name != "" {
		return fmt.Sprintf("%s (%s)", ev.Name, ev.ID)
	}
	return ev.ID
}
