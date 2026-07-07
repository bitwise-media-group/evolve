// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package results

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/bitwise-media-group/evolve/internal/encfmt"
	"github.com/bitwise-media-group/evolve/internal/evalspec"
)

// ErrSchemaTooNew reports a results or report file written by a newer evolve:
// this binary must never overwrite it. Callers that would rewrite the file
// propagate it; read-only consumers may degrade instead.
var ErrSchemaTooNew = errors.New("written by a newer evolve")

// Schema is the current results-file schema version. v2 made the per-eval
// result a superset of skill-creator's grading.json (expectations with
// text/passed/evidence, summary, timing) and renamed the cases section.
//
// v3 unlumped measured input: InputTokens is now fresh (uncached) input only,
// with cache reads and writes on their own fields. The number's meaning
// changed, so old files are discarded on load rather than silently
// reinterpreted (LoadDir returns reset=true on a version mismatch).
//
// The runtime-error fields (EvalResult.RuntimeError, EvalSummary.Errored) are
// additive and omitempty, so they do not change the schema number: old files
// load unchanged, and an older binary simply ignores them. They do shift the
// meaning of the failed count — an eval whose agent never produced usable
// output is now reported as errored rather than failed.
//
// v4 records the per-case assertion counts (derived from each result's grade
// summary) so a preserved eval row can show its Pass/Tot. A file one or more
// versions behind is upgraded in place on load (see migrate) rather than
// discarded, so committed history survives a schema bump; only an unreadable file
// or one written by a newer evolve still resets.
//
// v5 normalized the file shape: previous/baseline snapshots now carry a results
// array of the same TriggerResult/EvalResult struct as the live run (the compact
// per-case metric structs are gone), and the file is nested model-major under a
// models map (skill -> model -> {triggers, evals}) rather than tier-major. Old
// v3/v4 files are migrated in place on load (see migrate).
const Schema = 5

// File is one evals/<skill>/results.<ext>. It is nested model-major: each model
// key maps to a ModelEntry holding that model's trigger and eval entries, so the
// file reads in execution order (a model runs its triggers then its evals).
type File struct {
	Schema int                    `json:"schema"`
	Plugin string                 `json:"plugin"`
	Skill  string                 `json:"skill"`
	Models map[string]*ModelEntry `json:"models,omitempty"`
}

// ModelEntry groups one model's trigger and eval entries under its key. Either
// may be nil when that tier has not run for the model.
type ModelEntry struct {
	Triggers *TriggerEntry `json:"triggers,omitempty"`
	Evals    *EvalEntry    `json:"evals,omitempty"`
}

// Header is the run metadata common to both entry kinds.
type Header struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Display  string `json:"display"`
	// Harness names the agent CLI that executed this entry (claude, copilot, …).
	// Additive: empty on entries written before the harness split, when the
	// provider implied its driver.
	Harness     string `json:"harness,omitempty"`
	ToolVersion string `json:"tool_version"`
	RanAt       string `json:"ran_at"` // RFC3339 UTC, second precision
	Executed    bool   `json:"executed"`
	// ContentHash fingerprints the skill content this entry's tier depends on,
	// recorded when the entry was written: a trigger entry hashes the SKILL.md
	// frontmatter, an eval entry the whole skill directory. --modified reruns a
	// case when this differs from the current content (see the run package).
	// Empty on entries written before fingerprinting; --modified treats an empty
	// hash as "no baseline" and does not select on it.
	ContentHash    string   `json:"content_hash,omitempty"`
	RunsPerQuery   int      `json:"runs_per_query,omitempty"` // triggers only
	TimeoutSeconds int      `json:"timeout_seconds"`
	Pricing        *Pricing `json:"pricing"` // explicit null when unpriced
}

// Pricing snapshots the model's USD-per-MTok rates at run time.
type Pricing struct {
	InputPerMTok  *float64 `json:"input_per_mtok"`
	OutputPerMTok *float64 `json:"output_per_mtok"`
}

// Estimate is the counting-API figure for SKILL.md + prompt — the marginal
// context a triggering eval loads — priced at the model's input rate.
type Estimate struct {
	InputTokens  int      `json:"input_tokens"`
	InputCostUSD *float64 `json:"input_cost_usd,omitempty"`
}

// Measured is the harness-reported usage of a live case session. InputTokens
// is fresh (uncached) input only; cache reads and writes are reported
// separately so a multi-turn session's cheap cache traffic does not inflate
// the headline input figure. Total consumption is the sum of all three.
type Measured struct {
	InputTokens         *int     `json:"input_tokens,omitempty"`
	CacheReadTokens     *int     `json:"cache_read_input_tokens,omitempty"`
	CacheCreationTokens *int     `json:"cache_creation_input_tokens,omitempty"`
	OutputTokens        *int     `json:"output_tokens,omitempty"`
	CostUSD             *float64 `json:"cost_usd,omitempty"`
}

// TotalTokens is everything the session consumed: fresh input, cache reads,
// cache writes, and output. It returns nil when no token field is reported.
func (m *Measured) TotalTokens() *int {
	var total int
	var reported bool
	for _, t := range []*int{m.InputTokens, m.CacheReadTokens, m.CacheCreationTokens, m.OutputTokens} {
		if t != nil {
			total += *t
			reported = true
		}
	}
	if !reported {
		return nil
	}
	return &total
}

// TriggerEntry is one model's trigger sweep over a skill. Previous snapshots the
// run this entry replaced, so deltas (current vs previous) are computable without
// re-running. Triggers have no baseline: a query cannot trigger a skill that is
// not installed, so there is nothing to measure without it.
type TriggerEntry struct {
	Header
	Results  []TriggerResult  `json:"results"`
	Summary  TriggerSummary   `json:"summary"`
	Previous *TriggerSnapshot `json:"previous,omitempty"`
}

// TriggerSnapshot is a record of one prior trigger run: enough to compute deltas.
// Its Results carry the same TriggerResult struct as the live run — triggers have
// no bulky per-case detail to trim.
type TriggerSnapshot struct {
	RanAt   string          `json:"ran_at,omitempty"`
	Summary TriggerSummary  `json:"summary"`
	Results []TriggerResult `json:"results,omitempty"`
}

// TriggerResult is one query's outcome. Hits/Runs are exact integers (the
// rate and the 0.5 pass threshold are derived at render time).
type TriggerResult struct {
	Query         string    `json:"query"`
	ShouldTrigger bool      `json:"should_trigger"`
	Hits          *int      `json:"hits,omitempty"`
	Runs          *int      `json:"runs,omitempty"`
	Passed        *bool     `json:"passed,omitempty"`
	AvgRunSeconds *float64  `json:"avg_run_seconds,omitempty"`
	Estimate      *Estimate `json:"estimate,omitempty"`
	// SpecHash fingerprints this trigger's authored JSON definition when the
	// result was written; --modified reruns the query when it differs from the
	// current spec. Empty on pre-fingerprinting results (no baseline).
	SpecHash string `json:"spec_hash,omitempty"`
}

// TriggerSummary aggregates a trigger entry.
type TriggerSummary struct {
	Passed        *int      `json:"passed,omitempty"`
	Failed        *int      `json:"failed,omitempty"`
	Total         int       `json:"total"`
	PassRate      *float64  `json:"pass_rate,omitempty"`
	AvgRunSeconds *float64  `json:"avg_run_seconds,omitempty"`
	Estimate      *Estimate `json:"estimate,omitempty"`
}

// EvalEntry is one model's behavioral sweep over a skill. Baseline records the
// same evals run with the skill absent (the skill's lift over nothing); Previous
// snapshots the run this entry replaced (the iteration delta). Both are trimmed —
// summaries plus trimmed result arrays — so committed files stay readable.
type EvalEntry struct {
	Header
	Results  []EvalResult  `json:"results"`
	Summary  EvalSummary   `json:"summary"`
	Baseline *EvalSnapshot `json:"baseline,omitempty"`
	Previous *EvalSnapshot `json:"previous,omitempty"`
}

// EvalSnapshot is a record of one prior eval run (previous or baseline): the
// summary and a results array. Its Results carry the same EvalResult struct as
// the live run, trimmed of the bulky expectations/execution-metrics/timing detail
// (see trimEval).
type EvalSnapshot struct {
	RanAt   string       `json:"ran_at,omitempty"`
	Summary EvalSummary  `json:"summary"`
	Results []EvalResult `json:"results,omitempty"`
}

// GradedAssertion is one graded expectation: skill-creator's grading.json
// entry shape (text, passed, evidence) plus the authored assertion echoed
// for full fidelity. Text shadows the embedded assertion's authored text —
// llm entries carry it verbatim, deterministic checks a derived statement.
// Passed is tri-state: nil means skipped (e.g. a required binary is not
// installed). Source records which authored field produced the entry.
type GradedAssertion struct {
	evalspec.Assertion
	Text     string `json:"text"`
	Passed   *bool  `json:"passed"`
	Evidence string `json:"evidence"`
	Source   string `json:"source,omitempty"` // "expectation" or "assertion"
}

// GradeSummary aggregates one eval's graded expectations with grading.json's
// field names. PassRate excludes skips — passed/(passed+failed), identical
// to passed/total when nothing was skipped — and is omitted when nothing
// executed.
type GradeSummary struct {
	Passed   int      `json:"passed"`
	Failed   int      `json:"failed"`
	Total    int      `json:"total"`
	PassRate *float64 `json:"pass_rate,omitempty"`
	Skipped  int      `json:"skipped,omitempty"`
}

// Timing mirrors skill-creator's timing.json field names. Wave 1 populates
// the executor duration (the agent run; grading excluded) and the measured
// token total; the grader fields await grading instrumentation.
type Timing struct {
	TotalTokens             *int     `json:"total_tokens,omitempty"`
	DurationMS              *int     `json:"duration_ms,omitempty"`
	TotalDurationSeconds    *float64 `json:"total_duration_seconds,omitempty"`
	ExecutorStart           string   `json:"executor_start,omitempty"`
	ExecutorEnd             string   `json:"executor_end,omitempty"`
	ExecutorDurationSeconds *float64 `json:"executor_duration_seconds,omitempty"`
	GraderStart             string   `json:"grader_start,omitempty"`
	GraderEnd               string   `json:"grader_end,omitempty"`
	GraderDurationSeconds   *float64 `json:"grader_duration_seconds,omitempty"`
}

// ExecutionMetrics mirrors skill-creator's metrics.json field names;
// population arrives with transcript instrumentation (wave 2).
type ExecutionMetrics struct {
	ToolCalls         map[string]int `json:"tool_calls,omitempty"`
	TotalToolCalls    *int           `json:"total_tool_calls,omitempty"`
	TotalSteps        *int           `json:"total_steps,omitempty"`
	FilesCreated      []string       `json:"files_created,omitempty"`
	ErrorsEncountered *int           `json:"errors_encountered,omitempty"`
	OutputChars       *int           `json:"output_chars,omitempty"`
	TranscriptChars   *int           `json:"transcript_chars,omitempty"`
}

// EvalResult is one eval's outcome — a superset of a skill-creator
// grading.json document, plus evolve's identity, token, and cost extras.
type EvalResult struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// RuntimeError is a non-empty reason when the agent run produced no usable
	// output (auth blocked, crash, error envelope) — a runtime failure as
	// opposed to an eval that ran but failed assertions. When set, Passed is
	// nil: the eval neither passed nor failed.
	RuntimeError     string            `json:"runtime_error,omitempty"`
	Passed           *bool             `json:"passed,omitempty"`
	Estimate         *Estimate         `json:"estimate,omitempty"`
	Measured         *Measured         `json:"measured,omitempty"`
	Expectations     []GradedAssertion `json:"expectations,omitempty"`
	Summary          *GradeSummary     `json:"summary,omitempty"`
	ExecutionMetrics *ExecutionMetrics `json:"execution_metrics,omitempty"`
	Timing           *Timing           `json:"timing,omitempty"`
	// SpecHash fingerprints this eval's authored JSON definition when the result
	// was written; --modified reruns the eval when it differs from the current
	// spec. Empty on pre-fingerprinting results (no baseline).
	SpecHash string `json:"spec_hash,omitempty"`
	// Fingerprint is set only on baseline-snapshot results: the eval spec+fixtures
	// hash the baseline was run against, so a baseline is recomputed only when the
	// eval itself changes (not when the skill changes). Empty elsewhere.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// RunSeconds is the eval's executor (agent-run) duration, or nil when untimed.
func (r EvalResult) RunSeconds() *float64 {
	if r.Timing == nil {
		return nil
	}
	return r.Timing.ExecutorDurationSeconds
}

// EvalSummary aggregates an eval entry. Errored counts evals whose agent run
// failed to produce usable output (see EvalResult.RuntimeError); they are
// excluded from PassRate, like skips.
type EvalSummary struct {
	Passed        *int      `json:"passed,omitempty"`
	Failed        *int      `json:"failed,omitempty"`
	Errored       *int      `json:"errored,omitempty"`
	Total         int       `json:"total"`
	PassRate      *float64  `json:"pass_rate,omitempty"`
	AvgRunSeconds *float64  `json:"avg_run_seconds,omitempty"`
	Estimate      *Estimate `json:"estimate,omitempty"`
	Measured      *Measured `json:"measured,omitempty"`
}

// generatedComment heads results files in the formats that carry comments.
const generatedComment = "Maintained by evolve run; do not edit by hand."

// Find returns the path of the existing results file in dir, probing the
// supported extensions in discovery order, or "" when none exists.
func Find(dir string) string {
	for _, ext := range encfmt.Extensions {
		path := filepath.Join(dir, "results."+ext)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

// LoadDir finds and decodes the results file in dir regardless of format, or
// initialises a fresh one when the file is missing. A file written under an older
// structural schema (v3/v4) is migrated to the current shape on load (see migrate)
// and rewritten on the next SaveDir, so committed history survives a schema bump; a
// file that is unreadable or far older is discarded, and reset reports the discard
// so callers can tell the user history is starting over. A file written by a newer
// evolve is never discarded: LoadDir returns a fresh value alongside an error
// wrapping ErrSchemaTooNew, so writers refuse before touching it while read-only
// callers may blank the error and degrade.
func LoadDir(dir, plugin, skill string) (f *File, reset bool, err error) {
	fresh := &File{Schema: Schema, Plugin: plugin, Skill: skill}
	path := Find(dir)
	if path == "" {
		return fresh, false, nil
	}
	var probe struct {
		Schema int `json:"schema"`
	}
	if encfmt.DecodeFile(path, &probe) != nil {
		return fresh, true, nil
	}
	switch {
	case probe.Schema == Schema:
		var loaded File
		if encfmt.DecodeFile(path, &loaded) != nil {
			return fresh, true, nil
		}
		loaded.Plugin, loaded.Skill = plugin, skill
		return &loaded, false, nil
	case Migratable(probe.Schema):
		migrated, ok := migrate(path)
		if !ok {
			return fresh, true, nil
		}
		migrated.Plugin, migrated.Skill = plugin, skill
		return migrated, false, nil
	case probe.Schema > Schema:
		return fresh, false, fmt.Errorf("%s is schema %d, %w (this evolve writes schema %d); upgrade evolve",
			path, probe.Schema, ErrSchemaTooNew, Schema)
	default:
		// Older than the migratable range: discard.
		return fresh, true, nil
	}
}

// SaveDir writes results.<format> atomically with deterministic formatting,
// then removes stale results siblings left by a format switch. It returns
// the written path.
func (f *File) SaveDir(dir, format string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	format = encfmt.Canonical(format)
	if format == "" {
		format = "json"
	}
	data, err := encfmt.Marshal(f, format, generatedComment)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "results."+format)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	for _, ext := range encfmt.Extensions {
		if stale := filepath.Join(dir, "results."+ext); stale != path {
			_ = os.Remove(stale)
		}
	}
	return path, nil
}

// model returns the model entry for key, creating it (and the Models map) when
// absent.
func (f *File) model(key string) *ModelEntry {
	if f.Models == nil {
		f.Models = map[string]*ModelEntry{}
	}
	m := f.Models[key]
	if m == nil {
		m = &ModelEntry{}
		f.Models[key] = m
	}
	return m
}

// SetTrigger stores the trigger entry under the model key, creating the section.
func (f *File) SetTrigger(key string, entry *TriggerEntry) {
	f.model(key).Triggers = entry
}

// SetEval stores the eval entry under the model key, creating the section.
func (f *File) SetEval(key string, entry *EvalEntry) {
	f.model(key).Evals = entry
}

// Trigger returns the model's trigger entry, or nil when the model or tier is absent.
func (f *File) Trigger(key string) *TriggerEntry {
	if m := f.Models[key]; m != nil {
		return m.Triggers
	}
	return nil
}

// Eval returns the model's eval entry, or nil when the model or tier is absent.
func (f *File) Eval(key string) *EvalEntry {
	if m := f.Models[key]; m != nil {
		return m.Evals
	}
	return nil
}

// ModelKeys returns the file's model keys in sorted order.
func (f *File) ModelKeys() []string {
	keys := make([]string, 0, len(f.Models))
	for k := range f.Models {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Round1 rounds to 1 decimal (seconds), Round6 to 6 (costs) — always round
// before marshaling so committed files never carry float noise.
func Round1(x float64) float64 { return math.Round(x*10) / 10 }

// Round6 rounds to 6 decimals.
func Round6(x float64) float64 { return math.Round(x*1e6) / 1e6 }

// PricingOf snapshots a model's rates, or nil (serialized as an explicit
// null) when the model has no published pricing.
func PricingOf(inputPerMTok, outputPerMTok *float64) *Pricing {
	if inputPerMTok == nil && outputPerMTok == nil {
		return nil
	}
	return &Pricing{InputPerMTok: inputPerMTok, OutputPerMTok: outputPerMTok}
}

// NewEstimate builds an estimate from a token count and the model's input
// rate; nil when no count is available.
func NewEstimate(tokens *int, inputPerMTok *float64) *Estimate {
	if tokens == nil {
		return nil
	}
	e := &Estimate{InputTokens: *tokens}
	if inputPerMTok != nil {
		cost := Round6(float64(*tokens) / 1e6 * *inputPerMTok)
		e.InputCostUSD = &cost
	}
	return e
}

// SummarizeExpectations tallies one eval's graded expectations into the
// grading.json summary shape.
func SummarizeExpectations(graded []GradedAssertion) *GradeSummary {
	s := &GradeSummary{Total: len(graded)}
	for _, g := range graded {
		switch {
		case g.Passed == nil:
			s.Skipped++
		case *g.Passed:
			s.Passed++
		default:
			s.Failed++
		}
	}
	if executed := s.Passed + s.Failed; executed > 0 {
		rate := Round6(float64(s.Passed) / float64(executed))
		s.PassRate = &rate
	}
	return s
}

// SumEstimates totals per-result estimates; nil when none exist. The cost
// total is present only when at least one estimate carries a cost.
func SumEstimates(estimates []*Estimate) *Estimate {
	var tokens int
	var cost float64
	var hasTokens, hasCost bool
	for _, e := range estimates {
		if e == nil {
			continue
		}
		tokens += e.InputTokens
		hasTokens = true
		if e.InputCostUSD != nil {
			cost += *e.InputCostUSD
			hasCost = true
		}
	}
	if !hasTokens {
		return nil
	}
	sum := &Estimate{InputTokens: tokens}
	if hasCost {
		rounded := Round6(cost)
		sum.InputCostUSD = &rounded
	}
	return sum
}

// TrimEval returns a snapshot copy of an eval result: the scalars deltas and
// baselines need (verdict, expectation summary, run time, tokens, usage,
// fingerprint), without the bulky per-case detail (graded expectations, execution
// metrics, name) or the verbose timing breakdown beyond the executor duration.
func TrimEval(r EvalResult) EvalResult {
	t := EvalResult{
		ID:           r.ID,
		RuntimeError: r.RuntimeError,
		Passed:       r.Passed,
		Estimate:     r.Estimate,
		Measured:     r.Measured,
		Summary:      r.Summary,
		Fingerprint:  r.Fingerprint,
	}
	if r.Timing != nil && r.Timing.ExecutorDurationSeconds != nil {
		t.Timing = &Timing{ExecutorDurationSeconds: r.Timing.ExecutorDurationSeconds}
	}
	return t
}

// SnapshotEval captures an entry's current state — its summary and a trimmed copy
// of every result — as the "previous" of the run that replaces it. Because the
// merge preserves untouched cases, the entry is always a complete prior run, so
// the snapshot faithfully represents the last committed state. Returns nil for a
// nil or unexecuted entry (nothing meaningful to compare against).
func SnapshotEval(e *EvalEntry) *EvalSnapshot {
	if e == nil || !e.Executed {
		return nil
	}
	snap := &EvalSnapshot{RanAt: e.RanAt, Summary: e.Summary}
	for _, r := range e.Results {
		snap.Results = append(snap.Results, TrimEval(r))
	}
	return snap
}

// SnapshotTrigger is SnapshotEval for the trigger tier. Trigger results carry no
// bulky detail, so they are snapshotted whole.
func SnapshotTrigger(e *TriggerEntry) *TriggerSnapshot {
	if e == nil || !e.Executed {
		return nil
	}
	snap := &TriggerSnapshot{RanAt: e.RanAt, Summary: e.Summary}
	snap.Results = append(snap.Results, e.Results...)
	return snap
}

// SumMeasured totals per-case measured usage; nil when nothing was measured. Each
// field is summed independently and present only when at least one case reported
// it, so a provider that omits (say) cache figures does not get a spurious zero.
func SumMeasured(ms []*Measured) *Measured {
	var in, cacheRead, cacheCreation, out int
	var cost float64
	var hasIn, hasCacheRead, hasCacheCreation, hasOut, hasCost bool
	for _, m := range ms {
		if m == nil {
			continue
		}
		if m.InputTokens != nil {
			in += *m.InputTokens
			hasIn = true
		}
		if m.CacheReadTokens != nil {
			cacheRead += *m.CacheReadTokens
			hasCacheRead = true
		}
		if m.CacheCreationTokens != nil {
			cacheCreation += *m.CacheCreationTokens
			hasCacheCreation = true
		}
		if m.OutputTokens != nil {
			out += *m.OutputTokens
			hasOut = true
		}
		if m.CostUSD != nil {
			cost += *m.CostUSD
			hasCost = true
		}
	}
	if !hasIn && !hasCacheRead && !hasCacheCreation && !hasOut && !hasCost {
		return nil
	}
	sum := &Measured{}
	if hasIn {
		sum.InputTokens = &in
	}
	if hasCacheRead {
		sum.CacheReadTokens = &cacheRead
	}
	if hasCacheCreation {
		sum.CacheCreationTokens = &cacheCreation
	}
	if hasOut {
		sum.OutputTokens = &out
	}
	if hasCost {
		rounded := Round6(cost)
		sum.CostUSD = &rounded
	}
	return sum
}

// SummarizeEvalResults aggregates eval results into an eval summary, the same way
// a live run is tallied — used to summarize a baseline snapshot assembled from
// individual without-skill results. An errored result is one with a runtime error.
func SummarizeEvalResults(rs []EvalResult) EvalSummary {
	s := EvalSummary{Total: len(rs)}
	passed, failed, errored := 0, 0, 0
	var runSum float64
	var runCount int
	estimates := make([]*Estimate, 0, len(rs))
	measures := make([]*Measured, 0, len(rs))
	for _, r := range rs {
		switch {
		case r.RuntimeError != "":
			errored++
		case r.Passed != nil && *r.Passed:
			passed++
		case r.Passed != nil:
			failed++
		}
		if rs := r.RunSeconds(); rs != nil {
			runSum += *rs
			runCount++
		}
		estimates = append(estimates, r.Estimate)
		measures = append(measures, r.Measured)
	}
	s.Passed = &passed
	s.Failed = &failed
	if errored > 0 {
		s.Errored = &errored
	}
	if passed+failed > 0 {
		rate := Round6(float64(passed) / float64(passed+failed))
		s.PassRate = &rate
	}
	if runCount > 0 {
		avg := Round1(runSum / float64(runCount))
		s.AvgRunSeconds = &avg
	}
	s.Estimate = SumEstimates(estimates)
	s.Measured = SumMeasured(measures)
	return s
}
