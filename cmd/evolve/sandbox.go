// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/runner"
)

// resolveSandbox builds the filesystem-confinement policy for agent runs from
// the --no-sandbox flag and the config's sandbox block. It is on by default:
// agent CLIs run with full write authority, and only the OS sandbox keeps an
// escaping run from wandering into source repositories outside its workspace.
//
// protected_roots default to the parent of the repository under test — the
// directory that holds it and its sibling repos — so an unconfigured run still
// refuses to write outside the project tree. Teams whose repos share one root
// (e.g. ~/Repos) set sandbox.protected_roots in .evolve to cover all of it.
func resolveSandbox(repo *layout.Repo, noSandbox bool) (runner.Sandbox, error) {
	if noSandbox {
		return runner.Sandbox{}, nil
	}
	enabled := true
	if opts.Viper != nil && opts.Viper.IsSet("sandbox.enabled") {
		enabled = opts.Viper.GetBool("sandbox.enabled")
	}
	if !enabled {
		return runner.Sandbox{}, nil
	}

	var roots []string
	if opts.Viper != nil {
		roots = opts.Viper.GetStringSlice("sandbox.protected_roots")
	}
	if len(roots) == 0 {
		roots = []string{filepath.Dir(repo.Root)}
	}
	resolved := make([]string, 0, len(roots))
	for _, r := range roots {
		abs, err := expandRoot(r, repo.Root)
		if err != nil {
			return runner.Sandbox{}, err
		}
		resolved = append(resolved, abs)
	}
	return runner.Sandbox{Enabled: true, ProtectedRoots: resolved}, nil
}

// expandRoot turns a configured protected root into an absolute path: it
// expands environment variables and a leading ~, and resolves a relative path
// against the repository root.
func expandRoot(p, repoRoot string) (string, error) {
	p = os.ExpandEnv(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %q: %w", p, err)
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(repoRoot, p)
	}
	return filepath.Clean(p), nil
}
