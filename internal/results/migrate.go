// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package results

import (
	"fmt"
	"sort"

	"github.com/bitwise-media-group/evolve/internal/encfmt"
)

// Migratable reports whether a results file written under the given on-disk
// schema can be upgraded to the current Schema in place. It is the single source
// of the convertible range: schemas outside it — older than the oldest layout
// migrate understands, or newer than this binary — cannot be upgraded without
// discarding committed data. LoadDir and MigrateFile both gate on it.
func Migratable(schema int) bool {
	return schema == 3 || schema == 4
}

// MigrateFile upgrades the results file in dir to the current schema in place,
// rewriting it in format only when it was written under an older structural
// schema (see Migratable). It reports the schema found on disk (0 when dir holds
// no results file) and whether it rewrote the file: a file already at the current
// schema, or no file at all, leaves upgraded false and writes nothing. plugin and
// skill stamp the rewritten file's identity, as in LoadDir.
//
// A file that cannot be migrated without discarding committed data — unreadable,
// older than the migratable range, or written by a newer evolve — is left
// untouched and reported as an error, never overwritten; the newer-evolve case
// wraps ErrSchemaTooNew, matching LoadDir.
func MigrateFile(dir, plugin, skill, format string) (onDisk int, upgraded bool, err error) {
	path := Find(dir)
	if path == "" {
		return 0, false, nil
	}
	var probe struct {
		Schema int `json:"schema"`
	}
	if encfmt.DecodeFile(path, &probe) != nil {
		return 0, false, fmt.Errorf("%s: unreadable or malformed results file", path)
	}
	switch {
	case probe.Schema == Schema:
		return probe.Schema, false, nil
	case Migratable(probe.Schema):
		f, ok := migrate(path)
		if !ok {
			return probe.Schema, false, fmt.Errorf("%s: cannot decode schema %d results file", path, probe.Schema)
		}
		f.Plugin, f.Skill = plugin, skill
		if _, err := f.SaveDir(dir, format); err != nil {
			return probe.Schema, false, err
		}
		return probe.Schema, true, nil
	case probe.Schema > Schema:
		return probe.Schema, false, fmt.Errorf("%s is schema %d, %w (this evolve writes schema %d); upgrade evolve",
			path, probe.Schema, ErrSchemaTooNew, Schema)
	default:
		return probe.Schema, false, fmt.Errorf(
			"%s is schema %d, which this evolve cannot migrate (current schema %d)", path, probe.Schema, Schema)
	}
}

// migrate reads a pre-v5 results file (schema 3 or 4) and converts it to the
// current shape: tier-major maps (triggers/evals) become a model-major Models
// map, and each snapshot's per-case metrics map becomes a trimmed results array.
// It returns ok=false when the file cannot be decoded. The converted file is
// rewritten as v5 by the next SaveDir.
//
// The legacy* types below mirror only the parts of the old layout that changed
// shape; they exist solely for this one-time conversion and are deletable at the
// next structural schema bump.
func migrate(path string) (*File, bool) {
	var lf legacyFile
	if encfmt.DecodeFile(path, &lf) != nil {
		return nil, false
	}
	f := &File{Schema: Schema, Plugin: lf.Plugin, Skill: lf.Skill}
	for key, e := range lf.Triggers {
		if e == nil {
			continue
		}
		f.model(key).Triggers = &TriggerEntry{
			Header:   e.Header,
			Results:  e.Results,
			Summary:  e.Summary,
			Previous: migrateTriggerSnapshot(e.Previous),
		}
	}
	for key, e := range lf.Evals {
		if e == nil {
			continue
		}
		f.model(key).Evals = &EvalEntry{
			Header:   e.Header,
			Results:  e.Results,
			Summary:  e.Summary,
			Baseline: migrateEvalSnapshot(e.Baseline),
			Previous: migrateEvalSnapshot(e.Previous),
		}
	}
	return f, true
}

func migrateTriggerSnapshot(s *legacyTriggerSnapshot) *TriggerSnapshot {
	if s == nil {
		return nil
	}
	out := &TriggerSnapshot{RanAt: s.RanAt, Summary: s.Summary}
	for q, c := range s.Cases {
		out.Results = append(out.Results, TriggerResult{
			Query:         q,
			Hits:          c.Hits,
			Runs:          c.Runs,
			Passed:        c.Passed,
			AvgRunSeconds: c.AvgRunSeconds,
			Estimate:      c.Estimate,
		})
	}
	sort.Slice(out.Results, func(i, j int) bool { return out.Results[i].Query < out.Results[j].Query })
	return out
}

func migrateEvalSnapshot(s *legacyEvalSnapshot) *EvalSnapshot {
	if s == nil {
		return nil
	}
	out := &EvalSnapshot{RanAt: s.RanAt, Summary: s.Summary}
	for id, c := range s.Cases {
		r := EvalResult{
			ID:          id,
			Passed:      c.Passed,
			Estimate:    c.Estimate,
			Measured:    c.Measured,
			Fingerprint: c.Fingerprint,
		}
		if c.Errored {
			r.RuntimeError = "errored"
		}
		if c.PassRate != nil || c.AssertPassed != nil || c.AssertTotal != nil {
			gs := &GradeSummary{PassRate: c.PassRate}
			if c.AssertPassed != nil {
				gs.Passed = *c.AssertPassed
			}
			if c.AssertTotal != nil {
				gs.Total = *c.AssertTotal
			}
			r.Summary = gs
		}
		if c.AvgRunSeconds != nil {
			r.Timing = &Timing{ExecutorDurationSeconds: c.AvgRunSeconds}
		}
		out.Results = append(out.Results, r)
	}
	sort.Slice(out.Results, func(i, j int) bool { return out.Results[i].ID < out.Results[j].ID })
	return out
}

// legacyFile is the pre-v5 (schema 3/4) on-disk shape: tier-major maps with
// per-case snapshot metric maps. Current results (TriggerResult/EvalResult) and
// the summaries are unchanged, so they decode straight into the current types.
type legacyFile struct {
	Plugin   string                         `json:"plugin"`
	Skill    string                         `json:"skill"`
	Triggers map[string]*legacyTriggerEntry `json:"triggers"`
	Evals    map[string]*legacyEvalEntry    `json:"evals"`
}

type legacyTriggerEntry struct {
	Header
	Results  []TriggerResult        `json:"results"`
	Summary  TriggerSummary         `json:"summary"`
	Previous *legacyTriggerSnapshot `json:"previous"`
}

type legacyTriggerSnapshot struct {
	RanAt   string                       `json:"ran_at"`
	Summary TriggerSummary               `json:"summary"`
	Cases   map[string]legacyTriggerCase `json:"cases"`
}

type legacyTriggerCase struct {
	Hits          *int      `json:"hits"`
	Runs          *int      `json:"runs"`
	Passed        *bool     `json:"passed"`
	AvgRunSeconds *float64  `json:"avg_run_seconds"`
	Estimate      *Estimate `json:"estimate"`
}

type legacyEvalEntry struct {
	Header
	Results  []EvalResult        `json:"results"`
	Summary  EvalSummary         `json:"summary"`
	Baseline *legacyEvalSnapshot `json:"baseline"`
	Previous *legacyEvalSnapshot `json:"previous"`
}

type legacyEvalSnapshot struct {
	RanAt   string                    `json:"ran_at"`
	Summary EvalSummary               `json:"summary"`
	Cases   map[string]legacyEvalCase `json:"cases"`
}

type legacyEvalCase struct {
	Passed        *bool     `json:"passed"`
	PassRate      *float64  `json:"pass_rate"`
	AssertPassed  *int      `json:"assert_passed"`
	AssertTotal   *int      `json:"assert_total"`
	AvgRunSeconds *float64  `json:"avg_run_seconds"`
	Measured      *Measured `json:"measured"`
	Estimate      *Estimate `json:"estimate"`
	Errored       bool      `json:"errored"`
	Fingerprint   string    `json:"fingerprint"`
}
