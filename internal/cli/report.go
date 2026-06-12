// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package cli

import (
	"github.com/bitwise-media-group/evolve/internal/report"
	"github.com/bitwise-media-group/evolve/internal/version"
)

// Thresholds reads report.thresholds from config.
func (o *Options) Thresholds() report.Thresholds {
	th := report.Thresholds{Models: o.Viper.GetStringSlice("report.thresholds.models")}
	if o.Viper.IsSet("report.thresholds.triggers_min_pass_rate") {
		v := o.Viper.GetFloat64("report.thresholds.triggers_min_pass_rate")
		th.TriggersMinPassRate = &v
	}
	if o.Viper.IsSet("report.thresholds.cases_min_pass_rate") {
		v := o.Viper.GetFloat64("report.thresholds.cases_min_pass_rate")
		th.CasesMinPassRate = &v
	}
	return th
}

// RegenerateReports refreshes the Markdown/JSON reports after a sweep, the
// way the Python harness did from run_triggers/run_cases.
func (o *Options) RegenerateReports() error {
	repo, err := o.Repo()
	if err != nil {
		return err
	}
	providers, _, err := o.Providers()
	if err != nil {
		return err
	}
	_, err = report.Generate(report.Options{
		Repo:        repo,
		ToolVersion: version.Version,
		Providers:   providers,
	})
	return err
}
