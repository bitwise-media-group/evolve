// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

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

// liveFocus is the row the left pane pins to: the live (executing) case so the
// pane auto-follows execution. The left pane takes no user cursor — navigation
// lives in the Executing pane.
func (d dashboardModel) liveFocus(nodes []nodeRef) int {
	if i := d.liveNode(nodes); i >= 0 {
		return i
	}
	return 0
}

// liveNode is the node index of the most recent in-flight case, or -1.
func (d dashboardModel) liveNode(nodes []nodeRef) int {
	if !d.hasLast {
		return -1
	}
	u := d.unit(d.lastRef)
	if u == nil {
		return -1
	}
	ui := d.index[d.lastRef]
	for i, n := range nodes {
		if n.kind == nkCase && n.unitIdx == ui && u.cases[n.caseIdx].label == d.lastLabel {
			return i
		}
	}
	return -1
}

func (d dashboardModel) leftCount(nodes []nodeRef) string {
	if loc, ok := d.activeLoc(nodes, d.liveFocus(nodes)); ok {
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

func (d dashboardModel) buildNodeRefs() []nodeRef {
	var nodes []nodeRef
	for pi := range d.tree {
		pstarted, pdone := d.groupState(d.pluginUnits(pi))
		if !pstarted || pdone {
			nodes = append(nodes, nodeRef{kind: nkPlugin, pi: pi, collapsed: true})
			continue
		}
		nodes = append(nodes, nodeRef{kind: nkPlugin, pi: pi})
		for si := range d.tree[pi].skills {
			sstarted, sdone := d.groupState(d.skillUnits(pi, si))
			if !sstarted || sdone {
				nodes = append(nodes, nodeRef{kind: nkSkill, pi: pi, si: si, collapsed: true})
				continue
			}
			nodes = append(nodes, nodeRef{kind: nkSkill, pi: pi, si: si})
			for mi := range d.tree[pi].skills[si].models {
				mg := d.tree[pi].skills[si].models[mi]
				mstarted, mdone := d.groupState(mg.units)
				if !mstarted || mdone {
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

func (d dashboardModel) renderLeft(nodes []nodeRef, hl, w, h int) string {
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

	// Overflow: pin the active model's plugin/skill/model headers and scroll its
	// case rows beneath them.
	loc, ok := d.activeLoc(nodes, hl)
	if !ok {
		return strings.Join(window(lines, hl, h), "\n")
	}
	pin := d.headerLines(loc, w)
	var caseIdx []int
	for i, n := range nodes {
		if (n.kind == nkCase || n.kind == nkRule) && n.pi == loc.pi && n.si == loc.si && n.mi == loc.mi {
			caseIdx = append(caseIdx, i)
		}
	}
	avail := max(h-len(pin), 1)
	focus := 0
	for j, i := range caseIdx {
		if i == hl {
			focus = j
		}
	}
	caseLines := make([]string, len(caseIdx))
	for j, i := range caseIdx {
		caseLines[j] = lines[i]
	}
	start := scrollStart(len(caseLines), focus, avail)
	out := append([]string(nil), pin...)
	shown := 0
	for j := start; j < len(caseLines) && shown < avail; j++ {
		line := caseLines[j]
		if shown == 0 && start > 0 {
			line = mutedStyle.Render(fmt.Sprintf("  ┄ ▲ %d done above ┄", start))
		} else if shown == avail-1 && j < len(caseLines)-1 {
			line = mutedStyle.Render(fmt.Sprintf("  ┄ ▼ %d more below ┄", len(caseLines)-j))
		}
		out = append(out, line)
		shown++
	}
	return strings.Join(out, "\n")
}

// nodeLine renders one tree row.
func (d dashboardModel) nodeLine(n nodeRef, w int, hot bool) string {
	switch n.kind {
	case nkPlugin:
		return d.pluginLine(n.pi, w, hot)
	case nkSkill:
		sg := d.tree[n.pi].skills[n.si]
		return d.headerRow(d.aggGlyph(d.skillUnits(n.pi, n.si)), 1, sg.title, d.skillTail(n.pi, n.si), w, hot)
	case nkModel:
		mg := d.tree[n.pi].skills[n.si].models[n.mi]
		return d.headerRow(d.aggGlyph(mg.units), 2, shortKey(mg.key), d.modelTail(mg), w, hot)
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

// pluginLine renders a plugin row: name, a coloured progress bar, and the total
// case count. The bar carries the status, so the row has no separate marker.
func (d dashboardModel) pluginLine(pi int, w int, hot bool) string {
	gutter := " "
	if hot {
		gutter = selectedStyle.Render("›")
	}
	nameW, countW := d.pluginColW()
	name := d.tree[pi].name
	pass, fail, runc, total := d.pluginCaseCounts(pi)
	namePadded := name + strings.Repeat(" ", max(nameW-ansi.StringWidth(name), 0))
	if !hot {
		namePadded = mutedStyle.Render(namePadded)
	}
	count := mutedStyle.Render(fmt.Sprintf("%*d", countW, total))
	// Uniform columns so every bar starts and ends at the same position:
	// gutter(1) + space(1) + name(nameW) + "  "(2) + bar + space(1) + count(countW).
	barW := max(w-(5+nameW+countW), 4)
	bar := progressBar(pass, fail, runc, total, barW)
	return clip(gutter+" "+namePadded+"  "+bar+" "+count, w)
}

// pluginColW returns the uniform name and count column widths across all plugins,
// so every progress bar shares a start column and width regardless of how long
// the plugin name or total count is.
func (d dashboardModel) pluginColW() (nameW, countW int) {
	for pi := range d.tree {
		if n := ansi.StringWidth(d.tree[pi].name); n > nameW {
			nameW = n
		}
		_, _, _, total := d.pluginCaseCounts(pi)
		if c := len(fmt.Sprintf("%d", total)); c > countW {
			countW = c
		}
	}
	return nameW, countW
}

// headerRow renders a skill or model row: marker, label, and a muted tail.
func (d dashboardModel) headerRow(glyph string, depth int, label, tail string, w int, hot bool) string {
	gutter := " "
	if hot {
		gutter = selectedStyle.Render("›")
	}
	indent := strings.Repeat("  ", depth)
	labelStyled := label
	if !hot {
		labelStyled = mutedStyle.Render(label)
	}
	body := labelStyled
	if tail != "" {
		body += "  " + mutedStyle.Render(tail)
	}
	return clip(gutter+indent+glyph+" "+body, w)
}

// caseLine renders one trigger/eval row with its live metric columns.
func (d dashboardModel) caseLine(n nodeRef, w int, hot bool) string {
	c := d.units[n.unitIdx].cases[n.caseIdx]
	gutter := " "
	if hot {
		gutter = selectedStyle.Render("›")
	}
	prefix := gutter + "      " + d.glyph(c.status) + " "
	metric := caseMetric(c)
	label := c.label
	if c.kind == run.KindEvals {
		label = "eval: " + label
	}
	avail := max(w-ansi.StringWidth(prefix)-ansi.StringWidth(metric)-2, 6)
	label = truncate(label, avail)
	pad := max(avail-ansi.StringWidth(label), 0)
	body := label + strings.Repeat(" ", pad) + " " + metric
	if !hot {
		body = mutedStyle.Render(body)
	}
	return clip(prefix+body, w)
}

// caseMetric formats the trailing columns: pass-rate, avg time, tokens, cost.
// Triggers show input tokens; evals show ↑in/↓out.
func caseMetric(c *caseState) string {
	rate := caseRate(c)
	avg := fmtDurPtr(c.metrics.AvgRunSeconds)
	cost := fmtCostPtr(c.metrics.CostUSD)
	if c.kind == run.KindTriggers {
		return fmt.Sprintf("%5s %6s %6s %8s", rate, avg, fmtTokPtr(c.metrics.InputTokens), cost)
	}
	tok := "↑" + fmtTokPtr(c.metrics.InputTokens) + "/↓" + fmtTokPtr(c.metrics.OutputTokens)
	return fmt.Sprintf("%5s %6s %13s %8s", rate, avg, tok, cost)
}

func caseRate(c *caseState) string {
	if c.kind == run.KindTriggers {
		if c.metrics.Hits != nil && c.metrics.Runs != nil {
			return fmt.Sprintf("%d/%d", *c.metrics.Hits, *c.metrics.Runs)
		}
		return "—"
	}
	if c.metrics.AssertPassed != nil && c.metrics.AssertTotal != nil {
		return fmt.Sprintf("%d/%d", *c.metrics.AssertPassed, *c.metrics.AssertTotal)
	}
	return "—"
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

func (d dashboardModel) pluginCaseCounts(pi int) (pass, fail, runc, total int) {
	for _, ui := range d.pluginUnits(pi) {
		for _, c := range d.units[ui].cases {
			total++
			switch c.status {
			case stPass:
				pass++
			case stFail, stError:
				fail++
			case stRunning:
				runc++
			}
		}
	}
	return pass, fail, runc, total
}

func (d dashboardModel) skillTail(pi, si int) string {
	units := d.skillUnits(pi, si)
	if started, _ := d.groupState(units); !started {
		return "pending"
	}
	ps, tot := 0, 0
	for _, ui := range units {
		ps += d.units[ui].passed
		tot += d.units[ui].total
	}
	return fmt.Sprintf("%d/%d", ps, tot)
}

func (d dashboardModel) modelTail(mg modelGroup) string {
	if started, _ := d.groupState(mg.units); !started {
		return "pending"
	}
	ps, tot := 0, 0
	for _, ui := range mg.units {
		ps += d.units[ui].passed
		tot += d.units[ui].total
	}
	return fmt.Sprintf("%d/%d", ps, tot)
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

// activeLoc resolves the model whose context the left pane pins: the highlighted
// case's model, else the first running model.
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

func (d dashboardModel) headerLines(l loc, w int) []string {
	sg := d.tree[l.pi].skills[l.si]
	mg := sg.models[l.mi]
	return []string{
		d.pluginLine(l.pi, w, false),
		d.headerRow(d.aggGlyph(d.skillUnits(l.pi, l.si)), 1, sg.title, d.skillTail(l.pi, l.si), w, false),
		d.headerRow(d.aggGlyph(mg.units), 2, shortKey(mg.key), d.modelTail(mg), w, false),
	}
}
