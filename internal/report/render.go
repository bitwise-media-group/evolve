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
	agents := opts.Repo.IsAgentRepo()
	title, method := "Skill evaluations", methodology
	if agents {
		title, method = "Agent evaluations", agentMethodology
	}
	b.WriteString(generatedMarker + "\n\n# " + title + "\n\n" + method + "\n")
	b.WriteString(renderExcluded(excluded))

	for _, pf := range loaded {
		ps := summary.Plugins[pf.plugin.Name]
		if ps == nil {
			continue
		}
		group := pf.plugin.Name
		if agents && group == layout.AgentsGroup {
			group = "Agents"
		}
		fmt.Fprintf(&b, "\n## %s\n", group)
		if len(ps.Triggers) > 0 {
			b.WriteString("\n### Triggers\n\n")
			fmt.Fprintf(&b, "| %s | %s | Passed | Pass rate | Δ rate | Avg run | Input tokens | Est. input cost |\n",
				caps.providerCol, caps.modelCol)
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
			fmt.Fprintf(&b, "| %s | %s | Passed | Δ rate | Lift vs base | Avg run | Input tokens"+
				" | Est. input cost | Measured in/out | Cache rd/wr | Measured cost |\n", caps.providerCol, caps.modelCol)
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

	if opts.Repo.Kind == layout.Single || agents {
		for _, pf := range loaded {
			b.WriteString(renderDetail(pf, caps))
		}
	}
	return b.String()
}

func renderDetailPage(pf pluginFiles, caps capabilityMap, title string) string {
	return generatedMarker + "\n\n" + title + "\n" + renderDetail(pf, caps)
}

// renderDetail renders, per skill, one heading per trigger query and per eval
// with a table whose rows are the models — the case-major view that makes
// cross-model comparison on a single case the default. Columns mirror the
// rollup's, with the per-case verdict (Result) standing in for its passed-count.
func renderDetail(pf pluginFiles, caps capabilityMap) string {
	var b strings.Builder
	for _, f := range pf.files {
		if len(f.Models) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n", f.Skill)
		renderTriggerCases(&b, f, caps)
		renderEvalCases(&b, f, caps)
	}
	return b.String()
}

// triggerKeys returns the model keys with trigger results in this file, sorted.
func triggerKeys(f *results.File) []string {
	ks := map[string]*results.TriggerEntry{}
	for k, m := range f.Models {
		if m.Triggers != nil {
			ks[k] = m.Triggers
		}
	}
	return sortedKeys(ks)
}

// evalKeys returns the model keys with eval results in this file, sorted.
func evalKeys(f *results.File) []string {
	ks := map[string]*results.EvalEntry{}
	for k, m := range f.Models {
		if m.Evals != nil {
			ks[k] = m.Evals
		}
	}
	return sortedKeys(ks)
}

// renderTriggerCases renders a "#### {query}" subsection per trigger query, each
// a model-per-row table.
func renderTriggerCases(b *strings.Builder, f *results.File, caps capabilityMap) {
	keys := triggerKeys(f)
	if len(keys) == 0 {
		return
	}
	b.WriteString("\n### Triggers\n")
	for _, q := range orderedTriggerQueries(f, keys) {
		expected := false
		for _, key := range keys {
			if r, ok := findTrigger(f.Models[key].Triggers.Results, q); ok {
				expected = r.ShouldTrigger
				break
			}
		}
		fmt.Fprintf(b, "\n#### %s (expected: %s)\n\n", heading(q), yesNo(expected))
		fmt.Fprintf(b, "| %s | %s | Result | Rate | Δ rate | Avg run | Input tokens | Est. cost |\n",
			caps.providerCol, caps.modelCol)
		b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for _, key := range keys {
			entry := f.Models[key].Triggers
			r, ok := findTrigger(entry.Results, q)
			if !ok {
				continue
			}
			provName, modelID, _ := strings.Cut(key, "/")
			fmt.Fprintf(b, "| %s | %s (`%s`) | %s | %s | %s | %s | %s | %s |\n",
				caps.providerDisplay(provName), entry.Display, modelID,
				fmtVerdict(r.Passed), fmtHits(r.Hits, r.Runs),
				triggerCaseRateDelta(r, entry.Previous), fmtSecs(r.AvgRunSeconds),
				fmtEstimateTokens(r.Estimate, provName, caps),
				fmtEstimateCost(r.Estimate, entry.Pricing, provName, caps))
		}
	}
}

// renderEvalCases renders a "#### {eval}" subsection per eval, each a
// model-per-row table, with every model's runtime errors and failed assertions
// listed below.
func renderEvalCases(b *strings.Builder, f *results.File, caps capabilityMap) {
	keys := evalKeys(f)
	if len(keys) == 0 {
		return
	}
	b.WriteString("\n### Evals\n")
	for _, id := range orderedEvalIDs(f, keys) {
		title := id
		for _, key := range keys {
			if r, ok := findEval(f.Models[key].Evals.Results, id); ok && r.Name != "" {
				title = id + " — " + r.Name
				break
			}
		}
		fmt.Fprintf(b, "\n#### %s\n\n", heading(title))
		fmt.Fprintf(b, "| %s | %s | Result | Δ rate | Lift vs base | Avg run | Input tokens | Est. cost"+
			" | Measured in/out | Cache rd/wr | Measured cost |\n", caps.providerCol, caps.modelCol)
		b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for _, key := range keys {
			entry := f.Models[key].Evals
			r, ok := findEval(entry.Results, id)
			if !ok {
				continue
			}
			provName, modelID, _ := strings.Cut(key, "/")
			fmt.Fprintf(b, "| %s | %s (`%s`) | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
				caps.providerDisplay(provName), entry.Display, modelID,
				evalVerdict(r), evalCaseRateDelta(r, entry.Previous, entry.Baseline),
				evalCaseLiftVsBase(r, entry.Baseline), fmtSecs(runSeconds(r.Timing)),
				fmtEstimateTokens(r.Estimate, provName, caps), fmtEstimateCost(r.Estimate, entry.Pricing, provName, caps),
				fmtMeasuredInOut(r.Measured, provName, caps), fmtMeasuredCache(r.Measured, provName, caps),
				fmtMeasuredCostDetail(r.Measured, entry.Pricing, provName, caps))
		}
		// Each model's runtime errors and failed expectations, surfaced under the table.
		for _, key := range keys {
			r, ok := findEval(f.Models[key].Evals.Results, id)
			if !ok {
				continue
			}
			_, modelID, _ := strings.Cut(key, "/")
			if r.RuntimeError != "" {
				fmt.Fprintf(b, "\n- `%s` runtime error: %s\n", modelID, cell(r.RuntimeError, 160))
			}
			for _, a := range r.Expectations {
				if a.Passed != nil && !*a.Passed {
					fmt.Fprintf(b, "\n- `%s` failed `%s`: %s\n", modelID, a.Text, cell(a.Evidence, 160))
				}
			}
		}
	}
}

// orderedTriggerQueries lists every query across the skill's models in authored
// order — first-seen over the sorted model keys, since all models share the same
// triggers spec.
func orderedTriggerQueries(f *results.File, keys []string) []string {
	var order []string
	seen := map[string]bool{}
	for _, key := range keys {
		for _, r := range f.Models[key].Triggers.Results {
			if !seen[r.Query] {
				seen[r.Query] = true
				order = append(order, r.Query)
			}
		}
	}
	return order
}

// orderedEvalIDs is orderedTriggerQueries for the eval tier.
func orderedEvalIDs(f *results.File, keys []string) []string {
	var order []string
	seen := map[string]bool{}
	for _, key := range keys {
		for _, r := range f.Models[key].Evals.Results {
			if !seen[r.ID] {
				seen[r.ID] = true
				order = append(order, r.ID)
			}
		}
	}
	return order
}

func findTrigger(rs []results.TriggerResult, query string) (results.TriggerResult, bool) {
	for _, r := range rs {
		if r.Query == query {
			return r, true
		}
	}
	return results.TriggerResult{}, false
}

func findEval(rs []results.EvalResult, id string) (results.EvalResult, bool) {
	for _, r := range rs {
		if r.ID == id {
			return r, true
		}
	}
	return results.EvalResult{}, false
}

// heading sanitizes a value for a Markdown heading: newlines would break it,
// but pipes are literal there and need no escaping.
func heading(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
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

// evalCaseLiftVsBase is liftVsBase at case granularity: one eval's pass-rate lift
// over its without-skill baseline, or "—" when there is no baseline for it.
func evalCaseLiftVsBase(r results.EvalResult, base *results.EvalSnapshot) string {
	if base != nil {
		if bc, ok := findEval(base.Results, r.ID); ok {
			return fmtSignedRate(results.EvalCaseDelta(r, bc).Rate)
		}
	}
	return "—"
}

// evalCaseRateDelta renders one eval's expectation-rate delta with the basis
// fallback (previous, else baseline marked).
func evalCaseRateDelta(r results.EvalResult, prev, base *results.EvalSnapshot) string {
	if prev != nil {
		if pc, ok := findEval(prev.Results, r.ID); ok {
			return fmtSignedRate(results.EvalCaseDelta(r, pc).Rate)
		}
	}
	if base != nil {
		if bc, ok := findEval(base.Results, r.ID); ok {
			return fmtSignedRate(results.EvalCaseDelta(r, bc).Rate) + " (vs base)"
		}
	}
	return "—"
}

// triggerCaseRateDelta renders one query's trigger-rate delta vs the previous run
// (triggers have no baseline).
func triggerCaseRateDelta(r results.TriggerResult, prev *results.TriggerSnapshot) string {
	if prev != nil {
		if pc, ok := findTrigger(prev.Results, r.Query); ok {
			return fmtSignedRate(results.TriggerCaseDelta(r, pc, r.ShouldTrigger).Rate)
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
