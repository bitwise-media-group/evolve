// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"sort"

	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/report"
	"github.com/bitwise-media-group/evolve/internal/run"
	"github.com/bitwise-media-group/evolve/internal/version"
)

// Thresholds reads report.thresholds from config.
func (o *Options) Thresholds() report.Thresholds {
	th := report.Thresholds{Models: o.Viper.GetStringSlice("report.thresholds.models")}
	if o.Viper.IsSet("report.thresholds.triggers_min_pass_rate") {
		v := o.Viper.GetFloat64("report.thresholds.triggers_min_pass_rate")
		th.TriggersMinPassRate = &v
	}
	if o.Viper.IsSet("report.thresholds.evals_min_pass_rate") {
		v := o.Viper.GetFloat64("report.thresholds.evals_min_pass_rate")
		th.EvalsMinPassRate = &v
	}
	return th
}

// JUnitPath and CoberturaPath are the configured CI-artifact output paths
// (report.junit / report.cobertura), empty when unset.
func (o *Options) JUnitPath() string     { return o.Viper.GetString("report.junit") }
func (o *Options) CoberturaPath() string { return o.Viper.GetString("report.cobertura") }

// StrictConfig is the configured report.strict default (the --strict flag overrides).
func (o *Options) StrictConfig() bool { return o.Viper.GetBool("report.strict") }

// Coverage computes per-skill coverage and maps it to the report package's type
// — the single place run.Coverage is translated across the run→report seam.
// strict requires the whole resolved model matrix per skill.
func (o *Options) Coverage(repo *layout.Repo, strict bool) ([]report.SkillCoverage, error) {
	configured, err := o.ConfiguredModels()
	if err != nil {
		return nil, err
	}
	cov, err := run.Coverage(repo, configured, strict)
	if err != nil {
		return nil, err
	}
	out := make([]report.SkillCoverage, len(cov))
	for i, c := range cov {
		out[i] = report.SkillCoverage{
			Plugin: c.Plugin, Skill: c.Skill, SkillMD: c.SkillMD, Lines: c.Lines, Covered: c.Covered,
		}
	}
	return out, nil
}

// DefinedModelKeys is the configured model matrix as sorted keys — the strict
// --check denominator.
func (o *Options) DefinedModelKeys() ([]string, error) {
	configured, err := o.ConfiguredModels()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(configured))
	for _, m := range configured {
		keys = append(keys, m.Key())
	}
	sort.Strings(keys)
	return keys, nil
}

// RegenerateReports refreshes the Markdown/JSON reports after a sweep, the
// way the Python harness did from run_triggers/run_evals. It also emits the
// JUnit/Cobertura artifacts when their paths are configured, so a configured
// user gets them refreshed alongside the rollup.
func (o *Options) RegenerateReports() error {
	repo, err := o.Repo()
	if err != nil {
		return err
	}
	models, err := o.AvailableModels()
	if err != nil {
		return err
	}
	active, _, err := o.ActiveModelKeys()
	if err != nil {
		return err
	}
	cobertura := o.CoberturaPath()
	var coverage []report.SkillCoverage
	if cobertura != "" {
		if coverage, err = o.Coverage(repo, o.StrictConfig()); err != nil {
			return err
		}
	}
	_, err = report.Generate(report.Options{
		Repo:          repo,
		ToolVersion:   version.Version,
		Models:        models,
		Format:        o.ResultsFormat,
		ActiveModels:  active,
		JUnitPath:     o.JUnitPath(),
		CoberturaPath: cobertura,
		Coverage:      coverage,
	})
	return err
}
