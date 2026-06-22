// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package report

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/encfmt"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/results"
)

// renderExcluded renders the note listing models the `models` restriction
// excludes, or "" when nothing is excluded (no restriction, or every catalog
// model is active).
func renderExcluded(excluded []excludedProvider) string {
	if len(excluded) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Excluded models\n\n")
	b.WriteString("Only the configured `models` appear below; these are excluded from this report:\n\n")
	b.WriteString("| Provider | Excluded models |\n")
	b.WriteString("| --- | --- |\n")
	for _, e := range excluded {
		models := "all models"
		if !e.all {
			ids := make([]string, len(e.models))
			for i, id := range e.models {
				ids[i] = "`" + id + "`"
			}
			models = strings.Join(ids, ", ")
		}
		fmt.Fprintf(&b, "| %s | %s |\n", e.display, models)
	}
	return b.String()
}

func renderRoot(opts Options, loaded []pluginFiles, summary *Summary, caps capabilityMap,
	excluded []excludedProvider) string {
	var b strings.Builder
	b.WriteString(generatedMarker + "\n\n# Skill evaluations\n\n" + methodology + "\n")
	b.WriteString(renderExcluded(excluded))

	for _, pf := range loaded {
		ps := summary.Plugins[pf.plugin.Name]
		if ps == nil {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n", pf.plugin.Name)
		if len(ps.Triggers) > 0 {
			b.WriteString("\n### Triggers\n\n")
			b.WriteString("| Provider | Model | Passed | Pass rate | Δ rate | Avg run | Input tokens | Est. input cost |\n")
			b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
			for _, key := range sortedKeys(ps.Triggers) {
				m := ps.Triggers[key]
				fmt.Fprintf(&b, "| %s | %s (`%s`) | %s | %s | %s | %s | %s | %s |\n",
					caps.providerDisplay(m.Provider), m.Display, strings.TrimPrefix(key, m.Provider+"/"),
					fmtPassed(m), fmtRate(m.PassRate), rateDelta(m), fmtSecs(m.AvgRunSeconds),
					fmtTokensCell(m, caps), fmtEstCostCell(m, caps))
			}
		}
		if len(ps.Evals) > 0 {
			b.WriteString("\n### Evals\n\n")
			b.WriteString("| Provider | Model | Passed | Δ rate | Lift vs base | Avg run | Input tokens" +
				" | Est. input cost | Measured in/out | Cache rd/wr | Measured cost |\n")
			b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
			for _, key := range sortedKeys(ps.Evals) {
				m := ps.Evals[key]
				fmt.Fprintf(&b, "| %s | %s (`%s`) | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
					caps.providerDisplay(m.Provider), m.Display, strings.TrimPrefix(key, m.Provider+"/"),
					fmtPassed(m), rateDelta(m), liftVsBase(m), fmtSecs(m.AvgRunSeconds),
					fmtTokensCell(m, caps), fmtEstCostCell(m, caps),
					fmtMeasuredTokens(m, caps), fmtMeasuredCacheRollup(m, caps), fmtMeasuredCost(m, caps))
			}
		}
	}

	if opts.Repo.Kind == layout.Single {
		for _, pf := range loaded {
			b.WriteString(renderDetail(pf, caps))
		}
	}
	return b.String()
}

func renderDetailPage(pf pluginFiles, caps capabilityMap, title string) string {
	return generatedMarker + "\n\n" + title + "\n" + renderDetail(pf, caps)
}

// renderDetail renders per-model, per-skill tables with one row per query or
// eval.
func renderDetail(pf pluginFiles, caps capabilityMap) string {
	var b strings.Builder

	// Group model keys by provider, in first-seen sorted order.
	keys := map[string]bool{}
	for _, f := range pf.files {
		for k := range f.Triggers {
			keys[k] = true
		}
		for k := range f.Evals {
			keys[k] = true
		}
	}
	for _, key := range sortedKeys(keys) {
		provName, modelID, _ := strings.Cut(key, "/")
		fmt.Fprintf(&b, "\n## %s — `%s`\n", caps.providerDisplay(provName), modelID)

		for _, f := range pf.files {
			if entry, ok := f.Triggers[key]; ok {
				fmt.Fprintf(&b, "\n### Triggers — %s\n\n", f.Skill)
				fmt.Fprintf(&b, "%s\n\n", lastRunNote(entry.Header, entry.RunsPerQuery))
				b.WriteString("| Query | Expected | Rate | Δ rate | Result | Avg run | Input tokens | Est. cost |\n")
				b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
				for _, r := range entry.Results {
					fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s |\n",
						cell(r.Query, 60), yesNo(r.ShouldTrigger), fmtHits(r.Hits, r.Runs),
						triggerCaseRateDelta(r, entry.Previous), fmtVerdict(r.Passed), fmtSecs(r.AvgRunSeconds),
						fmtEstimateTokens(r.Estimate, provName, caps), fmtEstimateCost(r.Estimate, entry.Pricing, provName, caps))
				}
			}
			if entry, ok := f.Evals[key]; ok {
				fmt.Fprintf(&b, "\n### Evals — %s\n\n", f.Skill)
				fmt.Fprintf(&b, "%s\n\n", lastRunNote(entry.Header, 0))
				b.WriteString("| Eval | Result | Δ rate | Run | Input tokens | Est. cost" +
					" | Measured in/out | Cache rd/wr | Measured cost |\n")
				b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
				for _, r := range entry.Results {
					fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
						cell(r.ID, 60), evalVerdict(r), evalCaseRateDelta(r, entry.Previous, entry.Baseline),
						fmtSecs(runSeconds(r.Timing)),
						fmtEstimateTokens(r.Estimate, provName, caps), fmtEstimateCost(r.Estimate, entry.Pricing, provName, caps),
						fmtMeasuredInOut(r.Measured, provName, caps), fmtMeasuredCache(r.Measured, provName, caps),
						fmtMeasuredCostDetail(r.Measured, entry.Pricing, provName, caps))
				}
				// Runtime errors and failed expectations get surfaced under the table.
				for _, r := range entry.Results {
					if r.RuntimeError != "" {
						fmt.Fprintf(&b, "\n- `%s` runtime error: %s\n", r.ID, cell(r.RuntimeError, 160))
					}
					for _, a := range r.Expectations {
						if a.Passed != nil && !*a.Passed {
							fmt.Fprintf(&b, "\n- `%s` failed `%s`: %s\n", r.ID, a.Text, cell(a.Evidence, 160))
						}
					}
				}
			}
		}
	}
	return b.String()
}

func lastRunNote(h results.Header, runsPerQuery int) string {
	note := fmt.Sprintf("Last run %s (evolve %s, timeout %ds)", h.RanAt, h.ToolVersion, h.TimeoutSeconds)
	if runsPerQuery > 0 {
		note += fmt.Sprintf(", %d runs per query", runsPerQuery)
	}
	if !h.Executed {
		note += " — token counts only"
	}
	return note + "."
}

// runSeconds extracts the executor duration from a per-eval timing block.
func runSeconds(t *results.Timing) *float64 {
	if t == nil {
		return nil
	}
	return t.ExecutorDurationSeconds
}

// --- cell formatters -------------------------------------------------------

func fmtPassed(m *ModelRollup) string {
	if m.Passed == nil {
		return "—"
	}
	errored := 0
	if m.Errored != nil {
		errored = *m.Errored
	}
	s := fmt.Sprintf("%d/%d", *m.Passed, m.executed-errored)
	if errored > 0 {
		s += fmt.Sprintf(" (%d errored)", errored)
	}
	return s
}

func fmtRate(rate *float64) string {
	if rate == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", *rate*100)
}

// fmtSignedRate renders a pass/trigger-rate delta as a signed percentage, or "—"
// when not comparable. Markdown cannot color, so the sign carries the direction.
func fmtSignedRate(d *float64) string {
	if d == nil {
		return "—"
	}
	return fmt.Sprintf("%+.0f%%", *d*100)
}

// rateDelta renders a rollup's headline rate delta with the basis fallback used
// across the reports and TUI: previous when present, else the baseline (marked),
// else "—".
func rateDelta(m *ModelRollup) string {
	if m.PreviousDelta != nil && m.PreviousDelta.Rate != nil {
		return fmtSignedRate(m.PreviousDelta.Rate)
	}
	if m.BaselineDelta != nil && m.BaselineDelta.Rate != nil {
		return fmtSignedRate(m.BaselineDelta.Rate) + " (vs base)"
	}
	return "—"
}

// liftVsBase renders an eval rollup's pass-rate lift over its without-skill
// baseline — the headline measure of the skill's value.
func liftVsBase(m *ModelRollup) string {
	if m.BaselineDelta != nil {
		return fmtSignedRate(m.BaselineDelta.Rate)
	}
	return "—"
}

// evalCaseRateDelta renders one eval's expectation-rate delta with the basis
// fallback (previous, else baseline marked).
func evalCaseRateDelta(r results.EvalResult, prev, base *results.EvalSnapshot) string {
	cur := results.EvalCaseMetricsOf(r)
	if prev != nil {
		if pc, ok := prev.Cases[r.ID]; ok {
			return fmtSignedRate(results.EvalCaseDelta(cur, pc).Rate)
		}
	}
	if base != nil {
		if bc, ok := base.Cases[r.ID]; ok {
			return fmtSignedRate(results.EvalCaseDelta(cur, bc).Rate) + " (vs base)"
		}
	}
	return "—"
}

// triggerCaseRateDelta renders one query's trigger-rate delta vs the previous run
// (triggers have no baseline).
func triggerCaseRateDelta(r results.TriggerResult, prev *results.TriggerSnapshot) string {
	if prev != nil {
		if pc, ok := prev.Cases[r.Query]; ok {
			return fmtSignedRate(results.TriggerCaseDelta(results.TriggerCaseMetricsOf(r), pc, r.ShouldTrigger).Rate)
		}
	}
	return "—"
}

func fmtSecs(secs *float64) string {
	if secs == nil {
		return "—"
	}
	return fmt.Sprintf("%.1fs", *secs)
}

func fmtHits(hits, runs *int) string {
	if hits == nil || runs == nil {
		return "—"
	}
	return fmt.Sprintf("%d/%d", *hits, *runs)
}

func fmtVerdict(passed *bool) string {
	switch {
	case passed == nil:
		return "—"
	case *passed:
		return "PASS"
	default:
		return "FAIL"
	}
}

// evalVerdict renders an eval's verdict cell, distinguishing a runtime error
// (the agent run failed to produce a gradable answer) from a graded
// pass/fail/skip.
func evalVerdict(r results.EvalResult) string {
	if r.RuntimeError != "" {
		return "ERROR"
	}
	return fmtVerdict(r.Passed)
}

func fmtTokensCell(m *ModelRollup, caps capabilityMap) string {
	if m.Estimate != nil {
		return groupThousands(m.Estimate.InputTokens)
	}
	if !caps.counts[m.Provider] {
		return "n/a"
	}
	return "—"
}

func fmtEstCostCell(m *ModelRollup, caps capabilityMap) string {
	if m.Estimate != nil && m.Estimate.InputCostUSD != nil {
		return fmtUSD(*m.Estimate.InputCostUSD)
	}
	if !caps.counts[m.Provider] || !m.priced {
		return "n/a"
	}
	return "—"
}

func fmtMeasuredTokens(m *ModelRollup, caps capabilityMap) string {
	if m.Measured != nil && (m.Measured.InputTokens != nil || m.Measured.OutputTokens != nil) {
		return inOut(m.Measured)
	}
	if !caps.usage[m.Provider] {
		return "n/a"
	}
	return "—"
}

func fmtMeasuredCost(m *ModelRollup, caps capabilityMap) string {
	if m.Measured != nil && m.Measured.CostUSD != nil {
		return fmtUSD(*m.Measured.CostUSD)
	}
	if !caps.usage[m.Provider] || !m.priced {
		return "n/a"
	}
	return "—"
}

func fmtEstimateTokens(e *results.Estimate, provName string, caps capabilityMap) string {
	if e != nil {
		return groupThousands(e.InputTokens)
	}
	if !caps.counts[provName] {
		return "n/a"
	}
	return "—"
}

func fmtEstimateCost(e *results.Estimate, pricing *results.Pricing, provName string, caps capabilityMap) string {
	if e != nil && e.InputCostUSD != nil {
		return fmtUSD(*e.InputCostUSD)
	}
	if !caps.counts[provName] || pricing == nil {
		return "n/a"
	}
	return "—"
}

func fmtMeasuredInOut(m *results.Measured, provName string, caps capabilityMap) string {
	if m != nil && (m.InputTokens != nil || m.OutputTokens != nil) {
		return inOut(m)
	}
	if !caps.usage[provName] {
		return "n/a"
	}
	return "—"
}

func fmtMeasuredCostDetail(m *results.Measured, pricing *results.Pricing, provName string, caps capabilityMap) string {
	if m != nil && m.CostUSD != nil {
		return fmtUSD(*m.CostUSD)
	}
	if !caps.usage[provName] || pricing == nil {
		return "n/a"
	}
	return "—"
}

func inOut(m *results.Measured) string {
	in, out := "—", "—"
	if m.InputTokens != nil {
		in = groupThousands(*m.InputTokens)
	}
	if m.OutputTokens != nil {
		out = groupThousands(*m.OutputTokens)
	}
	return in + "/" + out
}

// fmtMeasuredCache renders a detail row's cache reads/writes, mirroring
// fmtMeasuredInOut's n/a (provider never reports usage) vs — (not yet run).
func fmtMeasuredCache(m *results.Measured, provName string, caps capabilityMap) string {
	if m != nil && (m.CacheReadTokens != nil || m.CacheCreationTokens != nil) {
		return cacheRdWr(m)
	}
	if !caps.usage[provName] {
		return "n/a"
	}
	return "—"
}

// fmtMeasuredCacheRollup is fmtMeasuredCache for a rollup row.
func fmtMeasuredCacheRollup(m *ModelRollup, caps capabilityMap) string {
	if m.Measured != nil && (m.Measured.CacheReadTokens != nil || m.Measured.CacheCreationTokens != nil) {
		return cacheRdWr(m.Measured)
	}
	if !caps.usage[m.Provider] {
		return "n/a"
	}
	return "—"
}

func cacheRdWr(m *results.Measured) string {
	rd, wr := "—", "—"
	if m.CacheReadTokens != nil {
		rd = groupThousands(*m.CacheReadTokens)
	}
	if m.CacheCreationTokens != nil {
		wr = groupThousands(*m.CacheCreationTokens)
	}
	return rd + "/" + wr
}

func fmtUSD(v float64) string {
	return fmt.Sprintf("$%.4f", v)
}

func groupThousands(n int) string {
	s := fmt.Sprint(n)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	return s + "," + strings.Join(parts, ",")
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// cell truncates and escapes a value for a Markdown table cell.
func cell(s string, max int) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// summaryComment heads the rollup in the formats that carry comments.
const summaryComment = "Generated by evolve; do not edit by hand. Regenerate with: evolve report"

// SummaryName is the rollup's file name for the chosen format.
func SummaryName(format string) string {
	format = encfmt.Canonical(format)
	if format == "" {
		format = "json"
	}
	return "EVALUATION." + format
}

// writeSummary emits the machine-readable rollup as EVALUATION.<format> and
// removes stale rollups left by a format switch (EVALUATION.md is separate).
func writeSummary(root, format string, v any) error {
	data, err := encfmt.Marshal(v, encfmt.Canonical(cmp.Or(format, "json")), summaryComment)
	if err != nil {
		return err
	}
	path := filepath.Join(root, SummaryName(format))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	for _, ext := range encfmt.Extensions {
		if stale := filepath.Join(root, "EVALUATION."+ext); stale != path {
			_ = os.Remove(stale)
		}
	}
	return nil
}
