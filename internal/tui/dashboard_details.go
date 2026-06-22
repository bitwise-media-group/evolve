// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/plan"
)

// The right-column "Details" pane: the selected execution's pinned header above
// its scrollable result (output + verdict, or the authored spec while pending).

// renderDetails draws the Details pane: the selected execution's pinned header
// (which execution, status, tokens) above its scrollable result (output +
// verdict). It always mirrors the Runs selection; the result scrolls with
// ctrl+d/ctrl+u and j/k while Details is focused.
func (d dashboardModel) renderDetails(w, h int) string {
	sel := d.currentRun()
	if sel < 0 {
		switch {
		case !d.started:
			return mutedStyle.Render("waiting to start…")
		case d.done:
			return mutedStyle.Render("run complete.")
		default:
			return mutedStyle.Render("no executions yet.")
		}
	}
	exec := d.execLog[sel]
	header := d.detailHeader(exec, w)
	resultH := max(h-len(header), 1)
	result := d.detailResult(exec, w)
	scroll := clampInt(d.detailScroll, 0, max(0, len(result)-resultH))

	var b strings.Builder
	for _, ln := range header {
		b.WriteString(ln)
		b.WriteString("\n")
	}
	b.WriteString(scrollWindow(result, scroll, resultH))
	return b.String()
}

// detailHeader is the pinned top of the result pane: which execution this is,
// its status, and token figures. It stays put while the result scrolls.
func (d dashboardModel) detailHeader(e execItem, w int) []string {
	u := d.unit(e.ref)
	if u == nil {
		return nil
	}
	c := u.byLabel[e.label]
	kind := "trigger"
	if e.ref.Kind == plan.KindEvals {
		kind = "eval"
	}
	out := []string{
		titleStyle.Render(clip(u.plugin+" / "+u.ref.Skill+" / "+shortKey(u.ref.Key), w)),
		clip(kind+": "+e.label, w),
	}
	st := stPending
	if c != nil {
		st = c.status
	}
	line := d.glyph(st) + " " + statusWord(st)
	if el, ok := d.inflightElapsed(e.ref, e.label); ok {
		line += mutedStyle.Render("  " + fmtDur(el))
	} else if c != nil && c.metrics.AvgRunSeconds != nil {
		line += mutedStyle.Render("  " + fmtDur(*c.metrics.AvgRunSeconds))
	}
	out = append(out, line)
	if c != nil {
		if tok := tokenLine(c); tok != "" {
			out = append(out, mutedStyle.Render(clip(tok, w)))
		}
	}
	out = append(out, "")
	return out
}

// detailResult is the scrollable body for one execution: the agent output (a
// capped head — the full text lives in the log file) and the grading verdict, or
// the authored spec while it has not produced output yet, then the open hints.
func (d dashboardModel) detailResult(e execItem, w int) []string {
	u := d.unit(e.ref)
	if u == nil {
		return nil
	}
	c := u.byLabel[e.label]
	var b strings.Builder
	if c != nil && c.output != "" {
		// Keep the prompt under test pinned above the agent's output, the same
		// way it leads the authored spec while the execution is still pending.
		d.writePrompt(&b, e, w)
		writeBlock(&b, "Output", c.output, w, 0)
	} else {
		d.writeSpec(&b, e, w)
	}
	if c != nil && strings.TrimSpace(c.verdict) != "" {
		writeBlock(&b, "Verdict", strings.TrimRight(c.verdict, "\n"), w, 0)
	}
	if c != nil {
		if hint := openHint(c); hint != "" {
			b.WriteString(mutedStyle.Render(clip(hint, w)))
		}
	}
	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
}

// openHint advertises the o/O keys when the execution has a retained workspace
// directory or output log to open.
func openHint(c *caseState) string {
	var parts []string
	if c.workdir != "" {
		parts = append(parts, "[o] open dir")
	}
	if c.logPath != "" {
		parts = append(parts, "[l] open log")
	}
	return strings.Join(parts, " · ")
}

// writeBlock writes a titled, width-wrapped block clipped to maxRows lines, with
// an ellipsis when the text overflows.
func writeBlock(b *strings.Builder, title, text string, w, maxRows int) {
	b.WriteString(headerDetailsStyle.Render(title))
	b.WriteString("\n")
	lines := strings.Split(lipgloss.NewStyle().Width(max(w, 10)).Render(text), "\n")
	clipped := false
	if maxRows > 0 && len(lines) > maxRows {
		lines = lines[:maxRows]
		clipped = true
	}
	for _, ln := range lines {
		b.WriteString(clip(ln, w))
		b.WriteString("\n")
	}
	if clipped {
		b.WriteString(mutedStyle.Render("…"))
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

// writePrompt writes the execution's leading prompt block — a trigger's Query
// or an eval's Prompt — that heads the Details body. It leads the authored spec
// while pending and is repeated above the Output once results return, so the
// prompt under test stays visible throughout.
func (d dashboardModel) writePrompt(b *strings.Builder, e execItem, w int) {
	wrap := lipgloss.NewStyle().Width(max(w, 10))
	if e.ref.Kind == plan.KindTriggers {
		b.WriteString(headerDetailsStyle.Render("Query"))
		b.WriteString("\n")
		b.WriteString(wrap.Render(e.label))
		b.WriteString("\n\n")
		return
	}
	ev := findEval(d.skillCat[e.ref.Skill], e.label)
	if ev == nil {
		return
	}
	b.WriteString(headerDetailsStyle.Render("Prompt"))
	b.WriteString("\n")
	b.WriteString(wrap.Render(ev.Prompt))
	b.WriteString("\n\n")
}

// writeSpec renders the authored spec for an execution that has not produced
// output yet (pending/running): the prompt block, expectations, files, and meta.
func (d dashboardModel) writeSpec(b *strings.Builder, e execItem, w int) {
	wrap := lipgloss.NewStyle().Width(max(w, 10))
	sc := d.skillCat[e.ref.Skill]
	d.writePrompt(b, e, w)
	if e.ref.Kind == plan.KindTriggers {
		b.WriteString(headerDetailsStyle.Render("Expected"))
		b.WriteString("\n")
		exp := "should NOT trigger this skill"
		if t := findTrigger(sc, e.label); t != nil && t.ShouldTrigger {
			exp = "should trigger this skill"
		}
		b.WriteString(mutedStyle.Render(exp))
		b.WriteString("\n\n")
		return
	}
	ev := findEval(sc, e.label)
	if ev == nil {
		return
	}
	if ev.ExpectedOutput != "" {
		b.WriteString(headerDetailsStyle.Render("Expected output"))
		b.WriteString("\n")
		b.WriteString(wrap.Render(ev.ExpectedOutput))
		b.WriteString("\n\n")
	}
	switch {
	case len(ev.Assertions) > 0:
		b.WriteString(headerDetailsStyle.Render("Assertions"))
		b.WriteString("\n")
		for _, a := range ev.Assertions {
			if a.FromExpectation {
				continue
			}
			b.WriteString(wrap.Render("• " + assertionLine(a)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	case len(ev.Expectations) > 0:
		b.WriteString(headerDetailsStyle.Render("Expectations"))
		b.WriteString("\n")
		for _, x := range ev.Expectations {
			b.WriteString(wrap.Render("• " + x))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(ev.Files) > 0 {
		b.WriteString(headerDetailsStyle.Render("Files"))
		b.WriteString("\n")
		for _, f := range ev.Files {
			b.WriteString(mutedStyle.Render("• " + f.Rel + " → " + f.Dest))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	var meta []string
	if ev.AllowedTools != "" {
		meta = append(meta, "tools: "+ev.AllowedTools)
	}
	if ev.MaxTurns > 0 {
		meta = append(meta, fmt.Sprintf("max turns: %d", ev.MaxTurns))
	}
	if len(meta) > 0 {
		b.WriteString(mutedStyle.Render(clip(strings.Join(meta, "   "), w)))
		b.WriteString("\n")
	}
}

func tokenLine(c *caseState) string {
	m := c.metrics
	if c.kind == plan.KindTriggers {
		if m.InputTokens == nil && m.CostUSD == nil {
			return ""
		}
		return "↑ " + fmtTokPtr(m.InputTokens) + " in    " + fmtCostPtr(m.CostUSD)
	}
	var parts []string
	if m.InputTokens != nil || m.OutputTokens != nil || m.CacheReadTokens != nil || m.CacheCreationTokens != nil {
		// In is fresh input; Total folds in cache reads/writes so it reflects
		// everything consumed — the In↔Total gap is the (cheap) cache traffic.
		tot := fmtTok(intOr0(m.InputTokens) + intOr0(m.CacheReadTokens) +
			intOr0(m.CacheCreationTokens) + intOr0(m.OutputTokens))
		seg := "↑ " + fmtTokPtr(m.InputTokens) + " in   ↓ " + fmtTokPtr(m.OutputTokens) + " out"
		if m.CacheReadTokens != nil || m.CacheCreationTokens != nil {
			seg += "   ⟳ " + fmtTokPtr(m.CacheReadTokens) + "/" + fmtTokPtr(m.CacheCreationTokens) + " cache"
		}
		parts = append(parts, seg+"   "+tot+" total")
	}
	if m.CostUSD != nil {
		parts = append(parts, fmtCostPtr(m.CostUSD))
	}
	return strings.Join(parts, "    ")
}

func findTrigger(sc *plan.SkillCatalog, query string) *evalspec.Trigger {
	if sc == nil {
		return nil
	}
	for i := range sc.Triggers {
		if sc.Triggers[i].Query == query {
			return &sc.Triggers[i]
		}
	}
	return nil
}

func findEval(sc *plan.SkillCatalog, id string) *evalspec.Eval {
	if sc == nil {
		return nil
	}
	for i := range sc.Evals {
		if sc.Evals[i].ID == id {
			return &sc.Evals[i]
		}
	}
	return nil
}

// assertionLine renders a one-line human description of an assertion.
func assertionLine(a evalspec.Assertion) string {
	switch a.Type {
	case "file_exists":
		return "file exists: " + a.Path
	case "file_absent":
		return "file absent: " + a.Path
	case "regex":
		return "output matches /" + a.Pattern + "/"
	case "not_regex":
		return "output lacks /" + a.Pattern + "/"
	case "command":
		return "command: " + a.Run
	case "llm":
		return a.Text
	default:
		return a.Type
	}
}
