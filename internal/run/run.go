// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/provider"
	"github.com/bitwise-media-group/evolve/internal/results"
	"github.com/bitwise-media-group/evolve/internal/runner"
	"github.com/bitwise-media-group/evolve/internal/tokencount"
)

// Runner abstracts agent execution so tests inject fakes; runner.Exec is the
// real implementation.
type Runner interface {
	Run(ctx context.Context, spec provider.CommandSpec, timeout time.Duration,
		onLine func([]byte) bool) (runner.Result, error)
}

// Options holds the sweep configuration the trigger and case engines share;
// TriggerOptions and CaseOptions embed it and add their engine's knobs.
type Options struct {
	Repo           *layout.Repo
	Selected       []provider.Selection
	Counter        *tokencount.Counter
	Runner         Runner
	SkillFilter    string
	Timeout        time.Duration
	Jobs           int
	CountOnly      bool
	New            bool
	KeepWorkspaces bool
	ToolVersion    string
	Now            func() time.Time
	Stdout         io.Writer
	Stderr         io.Writer
}

// header snapshots the run metadata every results entry records.
func (o *Options) header(sel provider.Selection, executed bool) results.Header {
	return results.Header{
		Provider:       sel.Provider.Name(),
		Model:          sel.Model.ID,
		Display:        sel.Model.Display,
		ToolVersion:    o.ToolVersion,
		RanAt:          o.Now().UTC().Format(time.RFC3339),
		Executed:       executed,
		TimeoutSeconds: int(o.Timeout.Seconds()),
		Pricing:        results.PricingOf(sel.Model.InputUSD, sel.Model.OutputUSD),
	}
}

func payload(skillMD []byte, prompt string) string {
	return string(skillMD) + "\n\n" + prompt
}

func unionSkillDirs(selected []provider.Selection) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, sel := range selected {
		for _, d := range sel.Provider.SkillDirs() {
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}
	return dirs
}

func avgSuffix(avg *float64) string {
	if avg == nil {
		return ""
	}
	return fmt.Sprintf(", avg run %.1fs", *avg)
}
