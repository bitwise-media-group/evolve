// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

// Local copies of patchy pkg/evaluation (the remote-evaluation wire
// contract). Keep field-for-field identical; E5 swaps these for the
// published import.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// SubmissionVersion is the accepted Submission.Version value.
const SubmissionVersion = "v1"

// AuthInfo is GET /api/v1/auth/info: everything the client's login flow
// needs to run OIDC discovery and the PKCE authorization-code exchange, so a
// user configures nothing but the service URL.
type AuthInfo struct {
	// Issuer is the OIDC issuer URL (discovery is derived from it).
	Issuer string `json:"issuer"`
	// ClientID of the public (PKCE, secret-less) client the login uses.
	ClientID string `json:"clientID"`
	// Scopes to request; the client unions in openid and offline_access.
	Scopes []string `json:"scopes,omitempty"`
	// Mode is the server's auth mode ("oidc" or "none"). Mode none needs no
	// login at all.
	Mode string `json:"mode"`
}

// WorkspaceRef names a content-addressed workspace bundle.
type WorkspaceRef struct {
	// Digest is the hex sha256 of the deterministic gzip tarball.
	Digest string `json:"digest"`
	// SizeBytes of the tarball.
	SizeBytes int64 `json:"sizeBytes,omitempty"`
}

// HarnessOption is one acceptable harness for a unit, in preference order.
type HarnessOption struct {
	// Harness id ("claude", "codex", …).
	Harness string `json:"harness"`
	// ModelID is the harness-native model id the unit's model maps to.
	ModelID string `json:"modelID,omitempty"`
}

// JudgeSpec binds the in-pod LLM judge. V1 constraint: the judge model must
// be runnable by the unit's own harness — the client validates this before
// submitting.
type JudgeSpec struct {
	// Model is the canonical provider-qualified judge model id.
	Model string `json:"model"`
	// ModelID is its harness-native id on the unit's harness.
	ModelID string `json:"modelID,omitempty"`
}

// UnitSpec is one evaluation unit: a skill evaluated on one model at one
// tier. The triggers/evals spec documents travel in the workspace bundle
// (the evals/<skill>/ tree), not here — the pod loads them with its own
// loaders, keeping submissions small and bundles self-contained.
type UnitSpec struct {
	// Skill under evaluation and the plugin it belongs to.
	Skill  string `json:"skill"`
	Plugin string `json:"plugin,omitempty"`
	// Tier of the run: 1 = triggers, 2 = evals.
	Tier int `json:"tier"`
	// Model is the canonical provider-qualified model id.
	Model string `json:"model"`
	// Harnesses that can run the model, in preference order; the scheduler
	// launches the first one enabled in the runner fleet.
	Harnesses []HarnessOption `json:"harnesses"`
	// Workspace bundle the unit runs against.
	Workspace WorkspaceRef `json:"workspace"`
	// TimeoutMS bounds one agent run's wall clock, per tier semantics.
	TimeoutMS int64 `json:"timeoutMS,omitempty"`
	// MaxTurns per agent run (0 = the runner default).
	MaxTurns int `json:"maxTurns,omitempty"`
	// RunsPerQuery repeats each trigger query (tier 1 only).
	RunsPerQuery int `json:"runsPerQuery,omitempty"`
	// Jobs is the in-pod case concurrency (0 = the runner default).
	Jobs int `json:"jobs,omitempty"`
	// Baseline also runs each executed eval without the skill (tier 2).
	Baseline bool `json:"baseline,omitempty"`
	// Judge grades llm assertions (tier 2); nil skips the judge.
	Judge *JudgeSpec `json:"judge,omitempty"`
	// Cases is the case allowlist (trigger queries or eval ids); nil runs
	// all. The client computes --new/--failed/--modified selection locally
	// and encodes the outcome here.
	Cases []string `json:"cases,omitempty"`
	// PriorEntry is the client's existing results entry for this unit,
	// opaque to patchy. It seeds fingerprints, previous-run snapshots, and
	// baselines in-pod, so those behave exactly as they do locally.
	PriorEntry json.RawMessage `json:"priorEntry,omitempty"`
	// ClientVersion is the submitting evolve's version, recorded for skew
	// diagnostics (warned, never enforced).
	ClientVersion string `json:"clientVersion,omitempty"`
}

// Submission is POST /api/v1/evaluations.
type Submission struct {
	// Version of this contract; must be SubmissionVersion.
	Version string `json:"version"`
	// Units to run.
	Units []UnitSpec `json:"units"`
	// TTLSeconds overrides the server's retention of the finished
	// evaluation (0 keeps the server default).
	TTLSeconds int64 `json:"ttlSeconds,omitempty"`
}

// SubmissionResponse is the 201 body: the Evaluation name to monitor and the
// child unit names, index-ordered.
type SubmissionResponse struct {
	Name  string   `json:"name"`
	Units []string `json:"units"`
}

// SubmissionError is a non-2xx body. MissingWorkspaces (with a 412) lists
// digests the client must upload before resubmitting.
type SubmissionError struct {
	Error             string   `json:"error"`
	MissingWorkspaces []string `json:"missingWorkspaces,omitempty"`
}

// EventPrefix marks an event line on the runner's stdout; everything else the
// pod prints goes to stderr.
const EventPrefix = "EVOLVE-EVENT: "

// EventVersion is the current event schema version.
const EventVersion = 1

// Pod input environment: where the prepare container staged the unit spec (a
// UnitSpec JSON document) and the extracted workspace bundle.
const (
	EnvUnitFile  = "EVOLVE_UNIT_FILE"
	EnvBundleDir = "EVOLVE_BUNDLE_DIR"
)

// Event types. Patchy interprets only TypeResult and TypeFatal; every other
// type is progress, relayed verbatim to the monitoring client, which replays
// it onto its local reporter.
const (
	TypeUnitStarted     = "unit_started"
	TypeUnitSkipped     = "unit_skipped"
	TypeItemStarted     = "item_started"
	TypeItemDone        = "item_done"
	TypeBaselineStarted = "baseline_started"
	TypeBaselineDone    = "baseline_done"
	TypeUnitFinished    = "unit_finished"
	TypeWarn            = "warn"
	TypeResult          = "result"
	TypeFatal           = "fatal"
)

// UnitRef identifies the unit an event belongs to, mirroring the client's
// plan.UnitRef plus the plugin.
type UnitRef struct {
	Plugin string `json:"plugin,omitempty"`
	Skill  string `json:"skill"`
	// Key is the provider-qualified model id.
	Key string `json:"key"`
	// Kind is "triggers" or "evals".
	Kind string `json:"kind"`
}

// ItemMetrics mirrors the client's plan.ItemMetrics: the per-item figures a
// live dashboard renders. All fields optional.
type ItemMetrics struct {
	Hits                *int     `json:"hits,omitempty"`
	Runs                *int     `json:"runs,omitempty"`
	AvgRunSeconds       *float64 `json:"avgRunSeconds,omitempty"`
	InputTokens         *int     `json:"inputTokens,omitempty"`
	CacheReadTokens     *int     `json:"cacheReadTokens,omitempty"`
	CacheCreationTokens *int     `json:"cacheCreationTokens,omitempty"`
	OutputTokens        *int     `json:"outputTokens,omitempty"`
	CostUSD             *float64 `json:"costUSD,omitempty"`
	AssertPassed        *int     `json:"assertPassed,omitempty"`
	AssertTotal         *int     `json:"assertTotal,omitempty"`
}

// ItemEvent carries the per-item payloads. unit_started uses Total/Runs/Mode;
// item_started uses Index/Label/Runs; item_done (and the baseline pair) uses
// Index/Label/Status/Detail/Output/Metrics. Local workspace and log paths
// never appear — they are meaningless outside the pod.
type ItemEvent struct {
	Index int    `json:"index,omitempty"`
	Label string `json:"label,omitempty"`
	Runs  int    `json:"runs,omitempty"`
	// Total and Mode ride on unit_started: the unit's case count and its
	// run mode ("run" or "count-only").
	Total int    `json:"total,omitempty"`
	Mode  string `json:"mode,omitempty"`
	// Status is pass|fail|skip|error (item_done only).
	Status  string       `json:"status,omitempty"`
	Detail  string       `json:"detail,omitempty"`
	Output  string       `json:"output,omitempty"`
	Metrics *ItemMetrics `json:"metrics,omitempty"`
}

// UnitSummary mirrors the client's run.UnitSummary — the rollup reported when
// a unit finishes.
type UnitSummary struct {
	Executed      bool     `json:"executed"`
	Passed        int      `json:"passed"`
	Failed        int      `json:"failed"`
	Errored       int      `json:"errored"`
	Total         int      `json:"total"`
	AvgRunSeconds *float64 `json:"avgRunSeconds,omitempty"`
}

// Event is one EVOLVE-EVENT line: the pod's progress and result stream.
type Event struct {
	V    int      `json:"v"`
	Type string   `json:"type"`
	Unit *UnitRef `json:"unit,omitempty"`
	// Item carries the per-item payloads (see ItemEvent).
	Item *ItemEvent `json:"item,omitempty"`
	// Sum rides on unit_finished.
	Sum *UnitSummary `json:"sum,omitempty"`
	// Msg is the unit_skipped reason or the warn text.
	Msg string `json:"msg,omitempty"`
	// Result rides on type=result: the unit's outcome, exactly once per
	// successful pod, as the last event before exit.
	Result *UnitResult `json:"result,omitempty"`
	// Error rides on type=fatal: the pod could not produce a result.
	Error string `json:"error,omitempty"`
}

// Encode renders the event as one stdout line (prefix included, newline
// excluded).
func (e Event) Encode() (string, error) {
	e.V = EventVersion
	raw, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("evaluation: encode event: %w", err)
	}
	return EventPrefix + string(raw), nil
}

// Decode recovers an event from one log line; ok is false for any line that
// is not an event. The prefix is found anywhere in the line, because
// Kubernetes log lines may carry timestamps or wrapping.
func Decode(line []byte) (Event, bool) {
	rest, found := bytes.CutPrefix(bytes.TrimSpace(line), []byte(EventPrefix))
	if !found {
		if i := strings.Index(string(line), EventPrefix); i >= 0 {
			rest = line[i+len(EventPrefix):]
		} else {
			return Event{}, false
		}
	}
	var e Event
	if err := json.Unmarshal(rest, &e); err != nil {
		return Event{}, false
	}
	if e.V != EventVersion || e.Type == "" {
		return Event{}, false
	}
	return e, true
}

// MaxEntryBytes bounds UnitResult.Entry. The pod truncates evidence fields
// until the marshalled entry fits — the entry is stored in a ConfigMap under
// Kubernetes' ~1MiB object cap, and this bound leaves headroom for the
// object's own metadata after gzip.
const MaxEntryBytes = 900 << 10

// MaxDetailLen bounds human-facing detail strings on the wire (mirrors the
// CR status bound).
const MaxDetailLen = 4096

// MaxOutputLen bounds relayed item output snippets, so one chatty case
// cannot bloat the progress stream.
const MaxOutputLen = 16 << 10

// TokenUsage is a unit's token and cost accounting, summed over its agent
// runs. Cost stays a float on the wire; CR statuses render it as a decimal
// string.
type TokenUsage struct {
	InputTokens         int64   `json:"inputTokens,omitempty"`
	OutputTokens        int64   `json:"outputTokens,omitempty"`
	CacheReadTokens     int64   `json:"cacheReadTokens,omitempty"`
	CacheCreationTokens int64   `json:"cacheCreationTokens,omitempty"`
	CostUSD             float64 `json:"costUSD,omitempty"`
}

// CaseStatus is one case's graded outcome, compact enough for CR status.
type CaseStatus struct {
	// ID is the trigger query or eval id.
	ID string `json:"id"`
	// Passed reports the graded outcome.
	Passed bool `json:"passed"`
}

// ResultSummary is the typed, bounded portion of a unit result — everything
// patchy stamps into the EvaluationUnit status.
type ResultSummary struct {
	CasesPassed  int `json:"casesPassed"`
	CasesFailed  int `json:"casesFailed"`
	CasesErrored int `json:"casesErrored"`
	// Cases lists per-case outcomes; the pod bounds the list (the CR caps
	// it at 256 entries).
	Cases []CaseStatus `json:"cases,omitempty"`
	// TokenUsage summed over the unit's agent runs.
	TokenUsage TokenUsage `json:"tokenUsage,omitempty"`
	// ElapsedMS is the unit's wall-clock duration.
	ElapsedMS int64 `json:"elapsedMS,omitempty"`
	// Outcome is "ok" for a completed unit (graded failures included) or a
	// short failure word ("error", "timeout") otherwise.
	Outcome string `json:"outcome"`
}

// UnitResult is the type=result payload: the unit's outcome plus the
// finished results entry.
type UnitResult struct {
	// Tier (1|2), Model, and Harness echo what actually ran, so the result
	// is self-contained.
	Tier    int    `json:"tier"`
	Model   string `json:"model"`
	Harness string `json:"harness"`
	// Failed reports whether any executed case failed — the client's
	// --strict signal, not a scheduling failure.
	Failed bool `json:"failed"`
	// Summary is the bounded, typed digest patchy stores in CR status.
	Summary ResultSummary `json:"summary"`
	// Entry is the finished results entry (the client's TriggerEntry or
	// EvalEntry, schema 5), OPAQUE to patchy: stored whole in the results
	// ConfigMap and handed back to the client, which merges it into its
	// local results file. At most MaxEntryBytes.
	Entry json.RawMessage `json:"entry,omitempty"`
}

// Truncate clips s to at most max bytes on a rune boundary, appending an
// ellipsis when it clipped. Used by the pod to bound detail/output strings
// and evidence fields until an entry fits MaxEntryBytes.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	const ellipsis = "…"
	cut := max - len(ellipsis)
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ellipsis
}

// SSE event names on GET /api/v1/evaluations/{name}/events.
//
// The stream is content-bearing with an explicit end event:
//
//   - On connect the server replays one "unit" event per child, so a
//     reconnecting client rebuilds its view without Last-Event-ID; updates
//     re-emit "unit" and the client applies them idempotently by name.
//   - "event" events relay raw in-pod progress lines tagged with the unit
//     name. They are best-effort and degradable: a client must render
//     correctly from "unit" events alone.
//   - "end" carries the final snapshot; the server closes after sending it.
const (
	SSEEventUnit  = "unit"
	SSEEventEvent = "event"
	SSEEventEnd   = "end"
)

// UnitStatusWire is one child's state, sent as an SSE "unit" event and
// embedded in snapshots. Phase values are Pending|Running|Complete|Failed.
type UnitStatusWire struct {
	// Name of the EvaluationUnit; the client's idempotency key.
	Name string `json:"name"`
	// Index within the submission, 0-based.
	Index int    `json:"index"`
	Phase string `json:"phase,omitempty"`
	// Harness resolved at launch.
	Harness string `json:"harness,omitempty"`
	// Reason a Failed unit failed:
	// WorkspaceLost|HarnessUnavailable|JobFailed|Aborted|ResultTooLarge.
	Reason string `json:"reason,omitempty"`
	// Detail explains a failure for humans.
	Detail string `json:"detail,omitempty"`
	// Summary is the bounded digest once the unit settled.
	Summary *ResultSummary `json:"summary,omitempty"`
	// Result is the full unit result — Entry included — present once the
	// unit settled with one.
	Result *UnitResult `json:"result,omitempty"`
}

// EventWire is an SSE "event" payload: one relayed in-pod event, tagged with
// the unit it came from.
type EventWire struct {
	// Unit is the EvaluationUnit name.
	Unit string `json:"unit"`
	// Event is the relayed in-pod event, verbatim.
	Event Event `json:"event"`
}

// EvaluationStatusWire is the GET snapshot and the SSE "end" payload.
type EvaluationStatusWire struct {
	Name string `json:"name"`
	// Phase is Pending|Running|Complete|Failed.
	Phase     string `json:"phase,omitempty"`
	Submitter string `json:"submitter,omitempty"`
	// Unit counters.
	UnitsTotal    int `json:"unitsTotal"`
	UnitsComplete int `json:"unitsComplete"`
	UnitsFailed   int `json:"unitsFailed"`
	// Units are the per-child states, index-ordered.
	Units []UnitStatusWire `json:"units,omitempty"`
}
