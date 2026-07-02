// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/encfmt"
	"github.com/bitwise-media-group/evolve/internal/model"
)

// configModelEntry is the providers.<id>.models[i] shape InjectModels writes —
// the same fields parseOverrideModel reads back. Fields matching the parse-time
// defaults (name = bare id, supported/preferred = the provider's native
// harness) are omitted so injected entries stay minimal.
type configModelEntry struct {
	ID        string            `json:"id"`
	Name      string            `json:"name,omitempty"`
	InputUSD  *float64          `json:"input_per_mtok,omitempty"`
	OutputUSD *float64          `json:"output_per_mtok,omitempty"`
	Supported map[string]string `json:"supported,omitempty"`
	Preferred string            `json:"preferred,omitempty"`
}

// InjectModels writes the chosen models into the repository's .evolve config
// file (creating .evolve.yaml when the repo has none) and returns the path
// written plus the canonical ids actually added — models already listed in the
// config are dropped rather than duplicated.
//
// Because a providers.<id>.models override replaces the builtin list for that
// provider instead of merging with it, a provider's first injected entry also
// seeds the list with that provider's builtin models: the effective matrix
// after injection is always a superset of the one before it.
func (o *Options) InjectModels(selected []model.Model) (path string, added []string, err error) {
	dir := o.Root
	if dir == "" {
		dir = "."
	}
	path, err = FindConfigFile(dir)
	if err != nil {
		return "", nil, err
	}
	exists := path != ""
	if !exists {
		path = filepath.Join(dir, ".evolve.yaml")
	}

	// The ids each provider's config list already carries; a provider absent
	// from this map has no models list, so its first write seeds the builtins.
	listed := map[string]map[string]bool{}
	if exists {
		var cfg struct {
			Providers map[string]struct {
				Models []struct {
					ID string `json:"id"`
				} `json:"models"`
			} `json:"providers"`
		}
		if err := encfmt.DecodeFile(path, &cfg); err != nil {
			return "", nil, err
		}
		for pid, p := range cfg.Providers {
			if p.Models == nil {
				continue
			}
			ids := map[string]bool{}
			for _, m := range p.Models {
				// Config ids may be bare or provider-qualified; compare bare.
				id := m.ID
				if _, rest, ok := strings.Cut(id, "/"); ok {
					id = rest
				}
				ids[id] = true
			}
			listed[pid] = ids
		}
	}

	for _, prov := range model.Providers() {
		var news []model.Model
		for _, m := range selected {
			if m.ProviderID == prov.ID && !listed[prov.ID][m.BareID()] {
				news = append(news, m)
			}
		}
		if len(news) == 0 {
			continue
		}
		var items []any
		if _, hasList := listed[prov.ID]; !hasList {
			for _, b := range model.AllModels(nil) {
				if b.ProviderID == prov.ID {
					items = append(items, entryFor(b))
				}
			}
		}
		for _, m := range news {
			items = append(items, entryFor(m))
			added = append(added, m.ID)
		}
		if err := encfmt.AppendToList(path, []string{"providers", prov.ID, "models"}, items); err != nil {
			return "", nil, err
		}
	}
	if len(added) == 0 {
		return path, nil, fmt.Errorf("nothing to add: every selected model is already in %s", filepath.Base(path))
	}
	return path, added, nil
}

// entryFor reduces a model to the config entry that round-trips through
// parseOverrideModel, dropping every field the parser would default anyway.
func entryFor(m model.Model) configModelEntry {
	e := configModelEntry{
		ID:        m.BareID(),
		Name:      m.Name,
		InputUSD:  m.InputUSD,
		OutputUSD: m.OutputUSD,
		Supported: m.Supported,
		Preferred: m.Preferred,
	}
	if e.Name == e.ID {
		e.Name = ""
	}
	native := nativeHarness[m.ProviderID]
	if len(m.Supported) == 0 || (len(m.Supported) == 1 && m.Supported[native] == m.BareID()) {
		e.Supported = nil
	}
	if m.Preferred == native || m.Preferred == "" {
		e.Preferred = ""
	}
	return e
}
