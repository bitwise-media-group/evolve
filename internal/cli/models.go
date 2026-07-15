// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/harness"
	"github.com/bitwise-media-group/evolve/internal/model"
	"github.com/bitwise-media-group/evolve/internal/tokencount"
)

// nativeHarness maps a provider id to the harness that natively drives it, used
// as the default Supported/Preferred for a config model override that omits them.
var nativeHarness = map[string]string{
	model.ProviderAnthropic: model.HarnessClaude,
	model.ProviderOpenAI:    model.HarnessCodex,
	model.ProviderGoogle:    model.HarnessGemini,
	model.ProviderCursor:    model.HarnessCursor,
	model.ProviderXAI:       model.HarnessGrok,
}

// ModelOverrides parses any providers.<name>.models lists from the config file
// into per-provider model replacements (replace, not merge). The second return
// reports which provider ids were overridden.
func (o *Options) ModelOverrides() (map[string][]model.Model, map[string]bool, error) {
	overrides := map[string][]model.Model{}
	raw := o.Viper.GetStringMap("providers")
	for name, v := range raw {
		entry, ok := v.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("config providers.%s: expected an object", name)
		}
		modelsRaw, ok := entry["models"]
		if !ok {
			continue
		}
		list, ok := modelsRaw.([]any)
		if !ok {
			return nil, nil, fmt.Errorf("config providers.%s.models: expected an array", name)
		}
		var models []model.Model
		for i, item := range list {
			m, err := parseOverrideModel(name, i, item)
			if err != nil {
				return nil, nil, err
			}
			models = append(models, m)
		}
		overrides[name] = models
	}
	overridden := map[string]bool{}
	for name := range overrides {
		overridden[name] = true
	}
	return overrides, overridden, nil
}

// parseOverrideModel builds one model.Model from a providers.<name>.models[i]
// config entry. id is required; name/pricing are optional; supported/preferred
// default to the provider's native harness when omitted.
func parseOverrideModel(provider string, i int, item any) (model.Model, error) {
	m, ok := item.(map[string]any)
	if !ok {
		return model.Model{}, fmt.Errorf("config providers.%s.models[%d]: expected an object", provider, i)
	}
	id, _ := m["id"].(string)
	if id == "" {
		return model.Model{}, fmt.Errorf("config providers.%s.models[%d]: missing id", provider, i)
	}
	out := model.Model{ProviderID: provider}
	if strings.Contains(id, "/") {
		out.ID = id
	} else {
		out.ID = provider + "/" + id
	}
	out.Name, _ = m["name"].(string)
	if out.Name == "" {
		out.Name, _ = m["display"].(string)
	}
	if out.Name == "" {
		out.Name = out.BareID()
	}
	if v, ok := toFloat(m["input_per_mtok"]); ok {
		out.InputUSD = &v
	}
	if v, ok := toFloat(m["output_per_mtok"]); ok {
		out.OutputUSD = &v
	}
	out.Supported, out.Preferred = parseSupported(m, provider, out.BareID())
	return out, nil
}

// parseSupported reads the optional `supported` map (harness id -> CLI model id)
// and `preferred` harness, defaulting both to the provider's native harness.
func parseSupported(m map[string]any, provider, bareID string) (map[string]string, string) {
	supported := map[string]string{}
	if raw, ok := m["supported"].(map[string]any); ok {
		for h, v := range raw {
			if s, ok := v.(string); ok {
				supported[h] = s
			}
		}
	}
	native := nativeHarness[provider]
	if len(supported) == 0 {
		if native != "" {
			supported[native] = bareID
		}
	}
	preferred, _ := m["preferred"].(string)
	if preferred == "" {
		if native != "" && supported[native] != "" {
			preferred = native
		} else {
			for h := range supported {
				preferred = h
				break
			}
		}
	}
	return supported, preferred
}

// AvailableModels returns the canonical model registry with any
// providers.<name>.models config overrides applied. It is the full set before
// the `models` restriction or harness availability is considered.
func (o *Options) AvailableModels() ([]model.Model, error) {
	overrides, _, err := o.ModelOverrides()
	if err != nil {
		return nil, err
	}
	return model.AllModels(overrides), nil
}

// ConfiguredModels returns the models this repo may use: AvailableModels
// narrowed by the `models` config restriction when set, else all of them.
func (o *Options) ConfiguredModels() ([]model.Model, error) {
	models, err := o.AvailableModels()
	if err != nil {
		return nil, err
	}
	restrict := o.Viper.GetStringSlice("models")
	if len(restrict) == 0 {
		return models, nil
	}
	var out []model.Model
	for _, m := range models {
		if m.MatchedBy(restrict) {
			out = append(out, m)
		}
	}
	return out, nil
}

// ModelsSpec resolves the --model flag into a Select spec, falling back to the
// `models` config restriction and then to "all".
func (o *Options) ModelsSpec(flag string) string {
	if flag != "" {
		return flag
	}
	if defaults := o.Viper.GetStringSlice("models"); len(defaults) > 0 {
		return strings.Join(defaults, ",")
	}
	return "all"
}

// RunnableSelections resolves the model spec and harness filter into the
// (model, harness) selections a sweep runs: models from ConfiguredModels bound
// to their runnable harness within the eligible (configured ∩ PATH ∩ --harness)
// set. Models with no eligible harness are dropped (see UnsupportedModelWarnings).
func (o *Options) RunnableSelections(modelsFlag, harnessFlag string) ([]harness.Selection, error) {
	models, err := o.ConfiguredModels()
	if err != nil {
		return nil, err
	}
	eligible, err := o.EligibleHarnesses(splitFlag(harnessFlag))
	if err != nil {
		return nil, err
	}
	return harness.Select(o.ModelsSpec(modelsFlag), models, eligible)
}

// ActiveModelKeys returns the set of "provider/model" keys allowed by the
// configuration (the `models` restriction, and/or the `harnesses` restriction),
// and whether any restriction is configured. PATH-independent: a configured
// model whose harness is not installed still counts as active so its committed
// results are not dropped. When nothing is configured, configured is false and
// callers must not filter.
func (o *Options) ActiveModelKeys() (keys map[string]bool, configured bool, err error) {
	allowedH, hConfigured := o.AllowedHarnessIDs()
	mConfigured := len(o.Viper.GetStringSlice("models")) > 0
	if !mConfigured && !hConfigured {
		return nil, false, nil
	}
	models, err := o.ConfiguredModels()
	if err != nil {
		return nil, false, err
	}
	allowed := map[string]bool{}
	for _, id := range allowedH {
		allowed[id] = true
	}
	keys = map[string]bool{}
	for _, m := range models {
		if hConfigured && !supportsAny(m, allowed) {
			continue
		}
		keys[m.Key()] = true
	}
	return keys, true, nil
}

// supportsAny reports whether m has a supported harness in the allowed set.
func supportsAny(m model.Model, allowed map[string]bool) bool {
	for id := range m.Supported {
		if allowed[id] {
			return true
		}
	}
	return false
}

// Counter builds the token-count cache, honoring a cache_dir config override.
func (o *Options) Counter(stderr interface{ Write([]byte) (int, error) }) (*tokencount.Counter, error) {
	path := o.Viper.GetString("cache_dir")
	if path != "" {
		path = path + "/token-counts.json"
	} else {
		var err error
		path, err = tokencount.DefaultCachePath()
		if err != nil {
			return nil, err
		}
	}
	return tokencount.New(path, stderr), nil
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	}
	return 0, false
}

// splitFlag splits a comma-separated flag value into trimmed, non-empty tokens.
func splitFlag(v string) []string {
	var out []string
	for t := range strings.SplitSeq(v, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}
