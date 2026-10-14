// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/harness"
)

// AllowedHarnessIDs returns the harness ids the `harnesses` config restriction
// permits, and whether it is configured. Nil ids with configured=false means
// "all harnesses" (the default).
func (o *Options) AllowedHarnessIDs() (ids []string, configured bool) {
	got := o.Viper.GetStringSlice("harnesses")
	if len(got) == 0 {
		return nil, false
	}
	return got, true
}

// Harnesses returns the harnesses the `harnesses` config restriction permits
// (all builtins when unset). An unknown configured id is an error.
func (o *Options) Harnesses() ([]harness.Harness, error) {
	allowed, configured := o.AllowedHarnessIDs()
	if !configured {
		return harness.All(), nil
	}
	var out []harness.Harness
	for _, id := range allowed {
		h, ok := harness.ByID(strings.TrimSpace(id))
		if !ok {
			return nil, fmt.Errorf("config harnesses: unknown harness %q", id)
		}
		out = append(out, h)
	}
	return out, nil
}

// AvailableHarnesses returns the configured harnesses whose CLI is on PATH.
func (o *Options) AvailableHarnesses() ([]harness.Harness, error) {
	configured, err := o.Harnesses()
	if err != nil {
		return nil, err
	}
	var out []harness.Harness
	for _, h := range configured {
		if _, ok := harness.Available(h); ok {
			out = append(out, h)
		}
	}
	return out, nil
}

// EligibleHarnesses returns the available harnesses narrowed by the --harness
// filter (a subset of the configured/available set). An empty filter means all
// available harnesses.
func (o *Options) EligibleHarnesses(filter []string) ([]harness.Harness, error) {
	available, err := o.AvailableHarnesses()
	if err != nil {
		return nil, err
	}
	if len(filter) == 0 {
		return available, nil
	}
	want := map[string]bool{}
	for _, id := range filter {
		want[strings.TrimSpace(id)] = true
	}
	var out []harness.Harness
	for _, h := range available {
		if want[h.ID()] {
			out = append(out, h)
		}
	}
	return out, nil
}

// ValidateFilterRestrictions errors when a --harness or --model filter value
// falls outside the configured `harnesses` / `models` restriction, or names an
// unknown harness/model. A filter may only narrow a restriction, never widen it.
//
// An agent repository has no model restriction to check (its selections are
// endpoints; ValidateSelectionFlags rejects --model/--harness there).
func (o *Options) ValidateFilterRestrictions(harnessFlag, modelFlag []string) error {
	if o.agentRepo() {
		return nil
	}
	knownH := map[string]bool{}
	for _, h := range harness.All() {
		knownH[h.ID()] = true
	}
	allowedH, hConfigured := o.AllowedHarnessIDs()
	allowedHSet := map[string]bool{}
	for _, id := range allowedH {
		allowedHSet[strings.TrimSpace(id)] = true
	}
	for _, raw := range harnessFlag {
		for _, id := range splitFlag(raw) {
			if !knownH[id] {
				return fmt.Errorf("--harness %q is not a known harness", id)
			}
			if hConfigured && !allowedHSet[id] {
				return fmt.Errorf("--harness %q is not in configured harnesses %v", id, allowedH)
			}
		}
	}

	if len(o.Viper.GetStringSlice("models")) == 0 {
		return nil // no models restriction → any existing model is allowed
	}
	configured, err := o.ConfiguredModels()
	if err != nil {
		return err
	}
	restrict := o.Viper.GetStringSlice("models")
	for _, raw := range modelFlag {
		for _, token := range splitFlag(raw) {
			if token == "all" {
				continue
			}
			matched := false
			for _, m := range configured {
				if token == m.ProviderID || token == m.ID || token == m.BareID() {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("--model %q is not in configured models %v", token, restrict)
			}
		}
	}
	return nil
}

// UnsupportedModelWarnings returns one warning per configured model that no
// configured-and-available harness can run, or nil when every configured model
// is runnable. Empty unless the `models` restriction is set (the auto-derived
// default set is runnable by construction).
func (o *Options) UnsupportedModelWarnings() ([]string, error) {
	if len(o.Viper.GetStringSlice("models")) == 0 {
		return nil, nil
	}
	if o.agentRepo() {
		return nil, nil // the root models config does not apply to an agent repository
	}
	models, err := o.ConfiguredModels()
	if err != nil {
		return nil, err
	}
	available, err := o.AvailableHarnesses()
	if err != nil {
		return nil, err
	}
	avail := map[string]bool{}
	for _, h := range available {
		avail[h.ID()] = true
	}
	var warnings []string
	for _, m := range models {
		if supportsAny(m, avail) {
			continue
		}
		warnings = append(warnings, fmt.Sprintf(
			"model %s has no available harness (supported by %s); it will not run",
			m.ID, strings.Join(m.SupportedHarnessIDs(), ", ")))
	}
	return warnings, nil
}
