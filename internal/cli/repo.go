// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/run"
)

// Repo detects the repository the global flags select.
func (o *Options) Repo() (*layout.Repo, error) {
	kind, err := layout.ParseKind(o.Layout)
	if err != nil {
		return nil, err
	}
	return layout.Detect(o.Root, kind)
}

// ChecksConfig layers the config file's checks.* overrides onto the defaults.
func (o *Options) ChecksConfig() run.CheckConfig {
	cfg := run.DefaultCheckConfig()
	v := o.Viper
	if v.IsSet("checks.license") {
		cfg.License = v.GetString("checks.license")
	}
	if v.IsSet("checks.description_pattern") {
		cfg.TriggerPattern = v.GetString("checks.description_pattern")
	}
	if v.IsSet("checks.max_skill_lines") {
		cfg.MaxSkillLines = v.GetInt("checks.max_skill_lines")
	}
	if v.IsSet("checks.ideal_skill_lines") {
		cfg.Signals.IdealSkillLines = v.GetInt("checks.ideal_skill_lines")
	}
	if v.IsSet("checks.signals") {
		cfg.Signals.Enabled = v.GetBool("checks.signals")
	}
	if v.IsSet("checks.plugin_manifests") {
		cfg.PluginManifests = v.GetStringSlice("checks.plugin_manifests")
	}
	if v.IsSet("checks.marketplace") {
		cfg.Marketplace = v.GetBool("checks.marketplace")
	}
	return cfg
}
