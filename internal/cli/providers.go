// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/tokencount"
)

// Providers returns the effective provider set: builtins with any
// providers.<name>.models lists from the config file replacing the builtin
// matrices. The second return reports which providers were overridden.
func (o *Options) Providers() ([]provider.Provider, map[string]bool, error) {
	overrides := map[string][]provider.Model{}
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
		var models []provider.Model
		for i, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("config providers.%s.models[%d]: expected an object", name, i)
			}
			model := provider.Model{}
			model.ID, _ = m["id"].(string)
			model.Display, _ = m["display"].(string)
			if model.ID == "" {
				return nil, nil, fmt.Errorf("config providers.%s.models[%d]: missing id", name, i)
			}
			if model.Display == "" {
				model.Display = model.ID
			}
			if v, ok := toFloat(m["input_per_mtok"]); ok {
				model.InputUSD = &v
			}
			if v, ok := toFloat(m["output_per_mtok"]); ok {
				model.OutputUSD = &v
			}
			models = append(models, model)
		}
		overrides[name] = models
	}

	overridden := map[string]bool{}
	for name := range overrides {
		overridden[name] = true
	}
	return provider.All(overrides), overridden, nil
}

// Selections resolves the providers and --models spec for a sweep.
func (o *Options) Selections(modelsFlag string) ([]provider.Selection, error) {
	providers, _, err := o.Providers()
	if err != nil {
		return nil, err
	}
	return provider.Select(o.ModelsSpec(modelsFlag), providers)
}

// ModelsSpec resolves the --models flag, falling back to the config's
// default_models list and then to "anthropic".
func (o *Options) ModelsSpec(flag string) string {
	if flag != "" {
		return flag
	}
	if defaults := o.Viper.GetStringSlice("default_models"); len(defaults) > 0 {
		var spec strings.Builder
		for i, d := range defaults {
			if i > 0 {
				spec.WriteString(",")
			}
			spec.WriteString(d)
		}
		return spec.String()
	}
	return "anthropic"
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
