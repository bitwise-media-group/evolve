// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// The left "Execution" pane: the plugin -> skill -> model -> case tree, its
// case rows, and the grouping/aggregation helpers that drive both.

// ── tree node model ─────────────────────────────────────────────────────────

type nodeKind int

const (
	nkPlugin nodeKind = iota
	nkSkill
	nkModel
	nkCase
	nkRule // horizontal divider between a model's trigger and eval rows
)

// nodeRef is one selectable row in the left tree. Completed/pending groups are a
// single collapsed row; an active group expands to its children, and an active
// model expands to its case rows.
type nodeRef struct {
	kind             nodeKind
	pi, si, mi       int
	unitIdx, caseIdx int
	collapsed        bool
}

// nodeKey identifies a collapsible group (plugin/skill/model) independent of its
// position in any particular node slice, so a user's browse-mode expansion
// survives the re-renders that live status triggers around it.
type nodeKey struct {
	kind       nodeKind
	pi, si, mi int
}

func keyOf(n nodeRef) nodeKey {
	return nodeKey{kind: n.kind, pi: n.pi, si: n.si, mi: n.mi}
}

// followHighlight is the row the left pane pins to while unfocused: the case of
// the shared selection (execLog[runSel]), so the Execution, Runs, and Details
// panes always agree on what is selected. Falls back to the top row when the
// selection is not currently visible in the tree.
func (d dashboardModel) followHighlight(nodes []nodeRef) int {
	if i := d.selectedNode(nodes); i >= 0 {
		return i
	}
	return 0
}

// selectedNode is the node index of the case the shared selection points at, or
// -1 when nothing is selected or that case isn't currently visible in the tree.
func (d dashboardModel) selectedNode(nodes []nodeRef) int {
	sel := d.currentRun()
	if sel < 0 {
		return -1
	}
	e := d.execLog[sel]
	ui, ok := d.index[e.ref]
	if !ok {
		return -1
	}
	for i, n := range nodes {
		if n.kind == nkCase && n.unitIdx == ui && d.units[ui].cases[n.caseIdx].label == e.label {
			return i
		}
	}
	return -1
}

// selLoc is the plugin/skill/model location of the shared selection's case, or
// ok=false when nothing is selected.
func (d dashboardModel) selLoc() (loc, bool) {
	sel := d.currentRun()
	if sel < 0 {
		return loc{}, false
	}
	ui, ok := d.index[d.execLog[sel].ref]
	if !ok {
		return loc{}, false
	}
	return d.locOfUnit(ui)
}

// locOfUnit finds the tree location (plugin/skill/model) of a unit index.
func (d dashboardModel) locOfUnit(ui int) (loc, bool) {
	for pi := range d.tree {
		for si := range d.tree[pi].skills {
			for mi, mg := range d.tree[pi].skills[si].models {
				if slices.Contains(mg.units, ui) {
					return loc{pi, si, mi}, true
				}
			}
		}
	}
	return loc{}, false
}

// groupContains reports whether loc falls within the plugin/skill/model group k.
func groupContains(k nodeKey, l loc) bool {
	switch k.kind {
	case nkPlugin:
		return k.pi == l.pi
	case nkSkill:
		return k.pi == l.pi && k.si == l.si
	case nkModel:
		return k.pi == l.pi && k.si == l.si && k.mi == l.mi
	}
	return false
}

func (d dashboardModel) leftCount(nodes []nodeRef, hl int) string {
	if loc, ok := d.activeLoc(nodes, hl); ok {
		mg := d.tree[loc.pi].skills[loc.si].models[loc.mi]
		done, total := 0, 0
		for _, ui := range mg.units {
			done += d.units[ui].done
			total += d.units[ui].total
		}
		return fmt.Sprintf("%s · %d/%d", shortKey(mg.key), done, total)
	}
	return ""
}

// ── rendering the tree ──────────────────────────────────────────────────────

// buildNodeRefs builds the follow-mode tree: groups expand purely from live
// status (a group is open only while started-and-not-done).
func (d dashboardModel) buildNodeRefs() []nodeRef {
	return d.buildNodeRefsWith(d.followExpanded)
}

// followExpanded is the live-status collapse rule used while the pane is not
// focused: a group is open while it is started and not yet done. When the user
// has paused on an explicit selection, the group holding it is opened too so the
// shared highlight stays visible; while following, completed groups still
// auto-collapse.
func (d dashboardModel) followExpanded(k nodeKey) bool {
	var units []int
	switch k.kind {
	case nkPlugin:
		units = d.pluginUnits(k.pi)
	case nkSkill:
		units = d.skillUnits(k.pi, k.si)
	case nkModel:
		units = d.tree[k.pi].skills[k.si].models[k.mi].units
	default:
		return false
	}
	if started, done := d.groupState(units); started && !done {
		return true
	}
	if !d.runFollow {
		if l, ok := d.selLoc(); ok && groupContains(k, l) {
			return true
		}
	}
	return false
}

// browseExpanded reads the user's expand/collapse overrides (browse mode).
func (d dashboardModel) browseExpanded(k nodeKey) bool { return d.execExpand[k] }

// execNodes is the browse-mode node slice, shared by every Execution key handler
// so the cursor and the view agree on indices.
func (d dashboardModel) execNodes() []nodeRef { return d.buildNodeRefsWith(d.browseExpanded) }

// buildNodeRefsWith flattens the plugin→skill→model→case tree into selectable
// rows, expanding each group according to expanded(key). Collapsed groups are a
// single row; an expanded model emits its case rows with one ruler between the
// trigger and eval blocks.
func (d dashboardModel) buildNodeRefsWith(expanded func(nodeKey) bool) []nodeRef {
	var nodes []nodeRef
	for pi := range d.tree {
		if !expanded(nodeKey{kind: nkPlugin, pi: pi}) {
			nodes = append(nodes, nodeRef{kind: nkPlugin, pi: pi, collapsed: true})
			continue
		}
		nodes = append(nodes, nodeRef{kind: nkPlugin, pi: pi})
		for si := range d.tree[pi].skills {
			if !expanded(nodeKey{kind: nkSkill, pi: pi, si: si}) {
				nodes = append(nodes, nodeRef{kind: nkSkill, pi: pi, si: si, collapsed: true})
				continue
			}
			nodes = append(nodes, nodeRef{kind: nkSkill, pi: pi, si: si})
			for mi := range d.tree[pi].skills[si].models {
				mg := d.tree[pi].skills[si].models[mi]
				if !expanded(nodeKey{kind: nkModel, pi: pi, si: si, mi: mi}) {
					nodes = append(nodes, nodeRef{kind: nkModel, pi: pi, si: si, mi: mi, collapsed: true})
					continue
				}
				nodes = append(nodes, nodeRef{kind: nkModel, pi: pi, si: si, mi: mi})
				ruled, trigShown := false, false
				for _, ui := range mg.units {
					for ci := range d.units[ui].cases {
						// One ruler separates the trigger block from the eval block.
						// Units within a model are ordered triggers-before-evals, so
						// the boundary is the first eval case after a trigger was shown.
						if !ruled && trigShown && d.units[ui].ref.Kind == run.KindEvals {
							nodes = append(nodes, nodeRef{kind: nkRule, pi: pi, si: si, mi: mi})
							ruled = true
						}
						nodes = append(nodes, nodeRef{kind: nkCase, pi: pi, si: si, mi: mi, unitIdx: ui, caseIdx: ci})
						if d.units[ui].ref.Kind == run.KindTriggers {
							trigShown = true
						}
					}
				}
			}
		}
	}
	return nodes
}

// leftHeader is the Execution pane's column header, mirroring the Rollup pane's
// aggHeader so the two read consistently. It labels the right-aligned metric
// block; the left title spans the tree column.
func (d dashboardModel) leftHeader(w int) string {
	right := metricCols("Pass/Tot", "Avg", "↑In", "↓Out", "Cost")
	tw := max(w-1-ansi.StringWidth(right)-1, 6)
	title := "Tree" + strings.Repeat(" ", max(tw-4, 0))
	return headerExecStyle.Render(clip(title+" "+right, w))
}

// renderLeft draws the Execution pane: a fixed column header above the scrollable
// tree body.
func (d dashboardModel) renderLeft(nodes []nodeRef, hl, w, h int) string {
	if h < 1 {
		h = 1
	}
	return d.leftHeader(w) + "\n" + d.renderLeftBody(nodes, hl, w, max(h-1, 1))
}

func (d dashboardModel) renderLeftBody(nodes []nodeRef, hl, w, h int) string {
	if h < 1 {
		h = 1
	}
	lines := make([]string, len(nodes))
	for i, n := range nodes {
		lines[i] = d.nodeLine(n, w, i == hl)
	}
	if len(lines) <= h {
		return strings.Join(lines, "\n")
	}
	// Scroll the whole tree to keep the highlight centred and on-screen — the same
	// in browse and follow modes. Pinning just the active model's subtree (the old
	// follow-mode path) made every other node vanish when the pane lost focus.
	return scrollWindow(lines, centerScroll(len(lines), hl, h), h)
}

// nodeLine renders one tree row.
func (d dashboardModel) nodeLine(n nodeRef, w int, hot bool) string {
	switch n.kind {
	case nkPlugin:
		units := d.pluginUnits(n.pi)
		metric, basis := d.groupMetric(units)
		return d.headerRow(d.aggGlyph(units), 0, d.tree[n.pi].name, metric, basis, w, hot)
	case nkSkill:
		sg := d.tree[n.pi].skills[n.si]
		units := d.skillUnits(n.pi, n.si)
		metric, basis := d.groupMetric(units)
		return d.headerRow(d.aggGlyph(units), 1, sg.title, metric, basis, w, hot)
	case nkModel:
		mg := d.tree[n.pi].skills[n.si].models[n.mi]
		metric, basis := d.groupMetric(mg.units)
		return d.headerRow(d.aggGlyph(mg.units), 2, shortKey(mg.key), metric, basis, w, hot)
	case nkRule:
		return ruleLine(w)
	default:
		return d.caseLine(n, w, hot)
	}
}

// ruleLine renders the muted divider between a model's trigger and eval rows,
// indented to sit under the case glyph column.
func ruleLine(w int) string {
	const indent = 8
	return strings.Repeat(" ", indent) + mutedStyle.Render(strings.Repeat("─", max(w-indent, 4)))
}

// headerRow renders a plugin, skill, or model row: marker, label, and a right-aligned
// rollup metric block in the same columns as the case rows below it. A completed
// group's metric carries delta colors and a baseline marker via basis.
func (d dashboardModel) headerRow(glyph string, depth int, label, metric string,
	basis deltaBasis, w int, hot bool,
) string {
	gutter := " "
	if hot {
		gutter = selectedStyle.Render("›")
	}
	prefix := gutter + strings.Repeat("  ", depth) + glyph + " "
	if basis == basisBaseline {
		label += " " + baselineMark
	}
	avail := max(w-ansi.StringWidth(prefix)-ansi.StringWidth(metric)-2, 6)
	label = truncate(label, avail)
	pad := max(avail-ansi.StringWidth(label), 0)
	return clip(prefix+joinRow(label, pad, metric, basis, hot), w)
}

// caseLine renders one trigger/eval row with its live metric columns, tinting the
// metrics by their delta once the case is complete.
func (d dashboardModel) caseLine(n nodeRef, w int, hot bool) string {
	u := d.units[n.unitIdx]
	c := u.cases[n.caseIdx]
	gutter := " "
	if hot {
		gutter = selectedStyle.Render("›")
	}
	prefix := gutter + "      " + d.caseGlyph(c) + " "
	label := c.label
	if c.kind == run.KindEvals {
		label = "eval: " + label
	}
	// During the baseline phase there are no metrics yet; render just the label in
	// yellow so the whole row reads as "running its baseline first".
	if c.baselineRunning {
		avail := max(w-ansi.StringWidth(prefix)-2, 6)
		return clip(prefix+baselineStyle.Render(truncate(label, avail)), w)
	}
	metric, basis := d.caseMetricCells(u.ref, c)
	if basis == basisBaseline {
		label += " " + baselineMark
	}
	avail := max(w-ansi.StringWidth(prefix)-ansi.StringWidth(metric)-2, 6)
	label = truncate(label, avail)
	pad := max(avail-ansi.StringWidth(label), 0)
	// Prior rows stay muted even when selected, so this-run vs last-run reads at a
	// glance; the gutter caret still marks the cursor.
	return clip(prefix+joinRow(label, pad, metric, basis, hot && !c.prior), w)
}

// joinRow assembles a tree row's label and metric block. A row carrying delta
// colors keeps its metric out of the unfocused-row muting so completed deltas stay
// visible; a plain row mutes as a whole when not selected.
func joinRow(label string, pad int, metric string, basis deltaBasis, hot bool) string {
	if basis != basisNone {
		labelPart := label + strings.Repeat(" ", pad)
		if !hot {
			labelPart = mutedStyle.Render(labelPart)
		}
		return labelPart + " " + metric
	}
	body := label + strings.Repeat(" ", pad) + " " + metric
	if !hot {
		body = mutedStyle.Render(body)
	}
	return body
}

// caseMetricCells renders a case's metric block, tinting each cell by its delta
// once the case is complete and a comparison basis exists. It returns the basis so
// the row can flag a baseline-based delta.
func (d dashboardModel) caseMetricCells(ref run.UnitRef, c *caseState) (string, deltaBasis) {
	rate := caseRate(c)
	avg := fmtDurPtr(c.metrics.AvgRunSeconds)
	in := fmtTokPtr(c.metrics.InputTokens)
	out := fmtTokPtr(c.metrics.OutputTokens)
	cost := fmtCostPtr(c.metrics.CostUSD)
	// Prior rows carry no live basis (nothing this run to compare), so they never
	// tint a delta — the dim row in caseLine is the only treatment.
	if c.prior || !c.status.terminal() {
		return metricCols(rate, avg, in, out, cost), basisNone
	}
	delta, basis := d.caseDelta(ref, c)
	if basis == basisNone {
		return metricCols(rate, avg, in, out, cost), basisNone
	}
	return colorMetricCells(rate, avg, in, out, cost, delta), basis
}

// colorMetricCells tints the five metric cells by their deltas (rate up = good;
// time/tokens/cost down = good) while preserving the metricCols layout.
func colorMetricCells(rate, avg, in, out, cost string, d results.Delta) string {
	return colorCell(fmt.Sprintf("%8s", rate), upGood(d.Rate)) + " " +
		colorCell(fmt.Sprintf("%6s", avg), downGood(d.AvgRunSeconds)) + " " +
		colorCell(fmt.Sprintf("%6s", in), downGoodInt(d.InputTokens)) + " " +
		colorCell(fmt.Sprintf("%6s", out), downGoodInt(d.OutputTokens)) + " " +
		colorCell(fmt.Sprintf("%7s", cost), downGood(d.CostUSD))
}

// metricColsFmt is the shared right-aligned metric block — Pass/Tot, Avg time,
// In tokens, Out tokens, Cost — used by case rows, the skill/model rollup rows,
// and the column header so every row's columns line up. All cells are single-
// width runes, so fmt's rune-count padding matches the on-screen width.
const metricColsFmt = "%8s %6s %6s %6s %7s"

// metricColsWidth is the rendered width of one metricCols block.
var metricColsWidth = ansi.StringWidth(metricCols("", "", "", "", ""))

func metricCols(passTot, avg, in, out, cost string) string {
	return fmt.Sprintf(metricColsFmt, passTot, avg, in, out, cost)
}

// groupMetric is the rolled-up metric block for a skill's or model's units,
// aligned to the same columns as the case rows. A not-yet-started group renders a
// right-aligned "pending"; a completed group tints its cells by the group delta
// and returns the basis so the header can flag a baseline comparison.
func (d dashboardModel) groupMetric(units []int) (string, deltaBasis) {
	started, done := d.groupState(units)
	if !started {
		return fmt.Sprintf("%*s", metricColsWidth, "pending"), basisNone
	}
	r := d.aggUnits(units)
	avg := emptyMetric
	if r.durN > 0 {
		avg = fmtDur(r.durSum / float64(r.durN))
	}
	cost := emptyMetric
	if r.hasCost {
		cost = fmtCost(r.cost)
	}
	passTot := fmt.Sprintf("%d/%d", r.passed, r.total)
	inTok, outTok := fmtTok(r.in), fmtTok(r.out)
	if !done {
		return metricCols(passTot, avg, inTok, outTok, cost), basisNone
	}
	delta, basis := d.aggregateGroup(units).delta()
	if basis == basisNone {
		return metricCols(passTot, avg, inTok, outTok, cost), basisNone
	}
	return colorMetricCells(passTot, avg, inTok, outTok, cost, delta), basis
}

func caseRate(c *caseState) string {
	if c.kind == run.KindTriggers {
		if c.metrics.Hits != nil && c.metrics.Runs != nil {
			// Pass count is correct runs: hits for a should-trigger query, the
			// non-firing runs for a should-not-trigger one — so a correct negative
			// reads 3/3, not 0/3.
			correct := *c.metrics.Hits
			if !c.shouldTrigger {
				correct = *c.metrics.Runs - *c.metrics.Hits
			}
			return fmt.Sprintf("%d/%d", correct, *c.metrics.Runs)
		}
		return emptyMetric
	}
	if c.metrics.AssertPassed != nil && c.metrics.AssertTotal != nil {
		return fmt.Sprintf("%d/%d", *c.metrics.AssertPassed, *c.metrics.AssertTotal)
	}
	return emptyMetric
}

// ── grouping + status helpers ───────────────────────────────────────────────

type loc struct{ pi, si, mi int }

func (d dashboardModel) pluginUnits(pi int) []int {
	out := make([]int, 0, len(d.units))
	for si := range d.tree[pi].skills {
		out = append(out, d.skillUnits(pi, si)...)
	}
	return out
}

func (d dashboardModel) skillUnits(pi, si int) []int {
	out := make([]int, 0, len(d.tree[pi].skills[si].models))
	for _, mg := range d.tree[pi].skills[si].models {
		out = append(out, mg.units...)
	}
	return out
}

func (d dashboardModel) groupState(unitIdxs []int) (started, done bool) {
	done = true
	for _, ui := range unitIdxs {
		s := d.units[ui].status
		if s != stPending {
			started = true
		}
		if !s.terminal() {
			done = false
		}
	}
	return started, done
}

func (d dashboardModel) aggStatus(unitIdxs []int) status {
	started, done := d.groupState(unitIdxs)
	if !started {
		return stPending
	}
	if !done {
		return stRunning
	}
	var anyFail, anyErr, anyPass, anyCount bool
	for _, ui := range unitIdxs {
		switch d.units[ui].status {
		case stError:
			anyErr = true
		case stFail:
			anyFail = true
		case stPass:
			anyPass = true
		case stCount:
			anyCount = true
		}
	}
	switch {
	case anyErr:
		return stError
	case anyFail:
		return stFail
	case anyPass:
		return stPass
	case anyCount:
		return stCount
	default:
		return stSkipped
	}
}

func (d dashboardModel) aggGlyph(unitIdxs []int) string { return d.glyph(d.aggStatus(unitIdxs)) }

// overallProgress tallies every case across the whole run into the segments of
// the top-right progress bar — settled-ok (pass, skipped, count-only), failed
// (fail/error), and running — plus the percent complete (all settled / total).
func (d dashboardModel) overallProgress() (ok, bad, running, total, pct int) {
	for _, u := range d.units {
		for _, c := range u.cases {
			if c.prior {
				continue // shown from a previous run; not this session's work
			}
			total++
			switch c.status {
			case stPass, stSkipped, stCount:
				ok++
			case stFail, stError:
				bad++
			case stRunning:
				running++
			}
		}
	}
	if total > 0 {
		pct = (ok + bad) * 100 / total
	}
	return ok, bad, running, total, pct
}

// overallProgressLine renders the run-wide progress bar shown above the Rollup
// pane: a full-width coloured bar with the percent-complete right-aligned. w is
// the rollup panel's outer width, so the bar lines up with the panels beneath it.
func (d dashboardModel) overallProgressLine(w int) string {
	ok, bad, running, total, pct := d.overallProgress()
	pctStr := fmt.Sprintf("%3d%%", pct)
	barW := max(w-ansi.StringWidth(pctStr)-1, 1)
	return progressBar(ok, bad, running, total, barW) + " " + mutedStyle.Render(pctStr)
}

// progressBar renders a width-char bar: green pass, red fail, yellow running,
// grey pending.
func progressBar(pass, fail, runc, total, width int) string {
	if width < 1 {
		return ""
	}
	if total < 1 {
		return pendStyle.Render(strings.Repeat("░", width))
	}
	gp := pass * width / total
	gf := fail * width / total
	gr := runc * width / total
	gpend := max(width-gp-gf-gr, 0)
	return passStyle.Render(strings.Repeat("█", gp)) +
		failStyle.Render(strings.Repeat("▓", gf)) +
		errStyle.Render(strings.Repeat("▒", gr)) +
		pendStyle.Render(strings.Repeat("░", gpend))
}

// activeLoc resolves the model the Execution pane's count label summarises: the
// highlighted case's model, else the first running model.
func (d dashboardModel) activeLoc(nodes []nodeRef, hl int) (loc, bool) {
	if hl >= 0 && hl < len(nodes) {
		n := nodes[hl]
		if n.kind == nkCase || (n.kind == nkModel && !n.collapsed) {
			return loc{n.pi, n.si, n.mi}, true
		}
	}
	for pi := range d.tree {
		for si := range d.tree[pi].skills {
			for mi := range d.tree[pi].skills[si].models {
				if started, done := d.groupState(d.tree[pi].skills[si].models[mi].units); started && !done {
					return loc{pi, si, mi}, true
				}
			}
		}
	}
	return loc{}, false
}

// ── browse-mode navigation ──────────────────────────────────────────────────
//
// While the Execution pane is focused, execSel is a cursor into the browse-mode
// node slice and execExpand records the user's open/closed groups. Every handler
// rebuilds that slice (it is cheap and rebuilt each render anyway) so the cursor
// and the view never disagree.

// execKey routes a key while the Execution pane is focused: cursor movement,
// expand/collapse, and opening a case in Details. Every move mirrors the cursor
// onto the shared selection so the Runs and Details panes track it; enter opens
// the case (which sets the selection itself) and changes focus.
func (d *dashboardModel) execKey(key string) {
	switch key {
	case "up", "k":
		d.moveExec(-1)
	case "down", "j":
		d.moveExec(1)
	case "g", "home":
		d.execTop()
	case "G", "end":
		d.execBottom()
	case "ctrl+d", "pgdown":
		d.moveExec(d.execPageStep())
	case "ctrl+u", "pgup":
		d.moveExec(-d.execPageStep())
	case "right":
		d.execExpandCurrent(true)
	case "left", "h":
		d.execExpandCurrent(false)
	case "enter", " ", "space":
		d.execActivate()
		return
	}
	d.syncSelFromExec()
}

// syncSelFromExec mirrors the browse cursor onto the shared selection: when the
// cursor sits on a case that has started (so it has a Runs-log row), that
// execution becomes the selection, and the Runs/Details panes follow. The cursor
// resting on a header or a not-yet-started case leaves the selection where it is.
func (d *dashboardModel) syncSelFromExec() {
	nodes := d.execNodes()
	if d.execSel < 0 || d.execSel >= len(nodes) {
		return
	}
	n := nodes[d.execSel]
	if n.kind != nkCase {
		return
	}
	c := d.units[n.unitIdx].cases[n.caseIdx]
	idx := d.execLogIndex(d.units[n.unitIdx].ref, c.label)
	if idx < 0 {
		return
	}
	if idx != d.runSel {
		d.detailScroll = 0
	}
	d.runSel = idx
	d.runFollow = idx == d.liveIdx
}

// syncExecToSel moves the browse cursor onto the shared selection's case,
// expanding its path so the row is visible. Used when the selection changes from
// outside the Execution pane (follow / a freshly-started execution) while it is
// focused.
func (d *dashboardModel) syncExecToSel() {
	if d.execExpand == nil {
		return
	}
	if l, ok := d.selLoc(); ok {
		d.execExpand[nodeKey{kind: nkPlugin, pi: l.pi}] = true
		d.execExpand[nodeKey{kind: nkSkill, pi: l.pi, si: l.si}] = true
		d.execExpand[nodeKey{kind: nkModel, pi: l.pi, si: l.si, mi: l.mi}] = true
	}
	if i := d.selectedNode(d.execNodes()); i >= 0 {
		d.execSel = i
	}
}

// moveExec moves the cursor by delta over selectable rows, skipping nkRule
// dividers and clamping to the visible tree.
func (d *dashboardModel) moveExec(delta int) {
	nodes := d.execNodes()
	if len(nodes) == 0 {
		d.execSel = 0
		return
	}
	d.execSel = clampInt(d.execSel, 0, len(nodes)-1)
	if delta == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	i := d.execSel
	for n := delta; n != 0; n -= step {
		j := i
		for {
			j += step
			if j < 0 || j >= len(nodes) {
				j = i // hit an edge: stay put
				break
			}
			if nodes[j].kind != nkRule {
				break
			}
		}
		i = j
	}
	d.execSel = i
}

func (d *dashboardModel) execTop() {
	d.execSel = 0
	d.skipRule(1)
}

func (d *dashboardModel) execBottom() {
	d.execSel = max(len(d.execNodes())-1, 0)
	d.skipRule(-1)
}

// skipRule nudges the cursor off an nkRule divider in the given direction.
func (d *dashboardModel) skipRule(step int) {
	nodes := d.execNodes()
	for d.execSel >= 0 && d.execSel < len(nodes) && nodes[d.execSel].kind == nkRule {
		d.execSel += step
	}
	d.execSel = clampInt(d.execSel, 0, max(len(nodes)-1, 0))
}

// execPageStep is roughly the left pane's visible row count, so ctrl+d/ctrl+u
// page by a screenful.
func (d dashboardModel) execPageStep() int {
	bodyH := max(d.h-3, 4)   // panel outer height (matches view(): title bar, blank, footer)
	inner := max(bodyH-2, 1) // height handed to renderLeft
	return max(inner-2, 1)   // minus the column header, minus one for context
}

// execActivate handles enter/space: toggle a group open/closed, or open a
// completed case in Details.
func (d *dashboardModel) execActivate() {
	nodes := d.execNodes()
	if d.execSel < 0 || d.execSel >= len(nodes) {
		return
	}
	n := nodes[d.execSel]
	switch n.kind {
	case nkPlugin, nkSkill, nkModel:
		k := keyOf(n)
		d.execExpand[k] = !d.execExpand[k]
		d.clampExecCursor()
	case nkCase:
		d.openCaseInDetails(n)
	}
}

// execExpandCurrent expands (right) or collapses (left) the cursor's group. On a
// case row, left collapses its parent model and moves the cursor onto it.
func (d *dashboardModel) execExpandCurrent(open bool) {
	nodes := d.execNodes()
	if d.execSel < 0 || d.execSel >= len(nodes) {
		return
	}
	n := nodes[d.execSel]
	switch n.kind {
	case nkPlugin, nkSkill, nkModel:
		d.execExpand[keyOf(n)] = open
		if !open {
			d.clampExecCursor()
		}
	case nkCase:
		if !open {
			mk := nodeKey{kind: nkModel, pi: n.pi, si: n.si, mi: n.mi}
			d.execExpand[mk] = false
			d.selectKey(mk)
		}
	}
}

// selectKey moves the cursor onto the row identified by k, if it is visible.
func (d *dashboardModel) selectKey(k nodeKey) {
	for i, n := range d.execNodes() {
		if keyOf(n) == k {
			d.execSel = i
			return
		}
	}
	d.clampExecCursor()
}

// clampExecCursor keeps the cursor in range and off an nkRule after the visible
// node set changes (e.g. a collapse).
func (d *dashboardModel) clampExecCursor() {
	nodes := d.execNodes()
	if len(nodes) == 0 {
		d.execSel = 0
		return
	}
	d.execSel = clampInt(d.execSel, 0, len(nodes)-1)
	if nodes[d.execSel].kind == nkRule {
		d.skipRule(-1)
	}
}

// execLogIndex finds the Runs-log row for a (unit, case) pair, newest first.
func (d dashboardModel) execLogIndex(ref run.UnitRef, label string) int {
	for i := len(d.execLog) - 1; i >= 0; i-- {
		if d.execLog[i].ref == ref && d.execLog[i].label == label {
			return i
		}
	}
	return -1
}

// openCaseInDetails selects the case's row in the Runs log and shows it in the
// Details pane, reusing the Runs→Details wiring. A case that has not started yet
// has no Runs row, so this is a no-op.
func (d *dashboardModel) openCaseInDetails(n nodeRef) {
	u := d.units[n.unitIdx]
	c := u.cases[n.caseIdx]
	if c.status == stPending {
		return
	}
	idx := d.execLogIndex(u.ref, c.label)
	if idx < 0 {
		return
	}
	d.setFocus(paneDetails) // exits browse, reverts the tree to follow
	d.runSel, d.runFollow, d.detailScroll = idx, false, 0
}
