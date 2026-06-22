// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"sort"

	"github.com/spf13/cobra"

	"github.com/bitwise-media-group/evolve/internal/results"
)

// StaleEntry is one skill's results file that holds models outside the
// configured `models` restriction.
type StaleEntry struct {
	dir  string
	file *results.File
	keys []string // stale "provider/model-id" keys in this file, sorted
}

// StaleModels returns the sorted, de-duplicated stale model keys across entries.
func StaleModels(entries []StaleEntry) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		for _, k := range e.keys {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

// FindStaleResults loads every results file in the repo and reports those
// holding models whose key is absent from active. active must come from a
// configured restriction (see ActiveModelKeys); callers skip this entirely
// when no restriction is configured.
func (o *Options) FindStaleResults(active map[string]bool) ([]StaleEntry, error) {
	repo, err := o.Repo()
	if err != nil {
		return nil, err
	}
	sets, err := repo.EvalSets()
	if err != nil {
		return nil, err
	}
	var out []StaleEntry
	for _, set := range sets {
		if results.Find(set.ResultsDir) == "" {
			continue
		}
		file, _ := results.LoadDir(set.ResultsDir, set.Plugin.Name, set.Skill)
		stale := map[string]bool{}
		for key := range file.Triggers {
			if !active[key] {
				stale[key] = true
			}
		}
		for key := range file.Evals {
			if !active[key] {
				stale[key] = true
			}
		}
		if len(stale) == 0 {
			continue
		}
		keys := make([]string, 0, len(stale))
		for k := range stale {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out = append(out, StaleEntry{dir: set.ResultsDir, file: file, keys: keys})
	}
	return out, nil
}

// DropStaleResults removes the stale model entries and rewrites each affected
// results file in the configured format.
func (o *Options) DropStaleResults(entries []StaleEntry) error {
	for _, e := range entries {
		for _, key := range e.keys {
			delete(e.file.Triggers, key)
			delete(e.file.Evals, key)
		}
		if _, err := e.file.SaveDir(e.dir, o.ResultsFormat); err != nil {
			return err
		}
	}
	return nil
}

// StaleResultsMode resolves how to treat results for models outside the
// configured `models` restriction: the --stale-results flag wins, then the
// stale_results config key, else "" (the caller prompts on a terminal or
// defaults to keep).
func (o *Options) StaleResultsMode(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup("stale-results"); f != nil && f.Changed {
		return f.Value.String()
	}
	return o.Viper.GetString("stale_results")
}
