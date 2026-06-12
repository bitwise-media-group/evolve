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
	if v.IsSet("checks.require_codex_manifest") {
		cfg.RequireCodexManifest = v.GetBool("checks.require_codex_manifest")
	}
	if v.IsSet("checks.forbid_hooks") {
		cfg.ForbidHooks = v.GetBool("checks.forbid_hooks")
	}
	if v.IsSet("checks.marketplace") {
		cfg.Marketplace = v.GetBool("checks.marketplace")
	}
	return cfg
}
