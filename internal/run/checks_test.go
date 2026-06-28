// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/layout"
)

func runChecks(t *testing.T, fixture string) []Finding {
	t.Helper()
	return runChecksCfg(t, fixture, DefaultCheckConfig())
}

func runChecksCfg(t *testing.T, fixture string, cfg CheckConfig) []Finding {
	t.Helper()
	repo, err := layout.Detect(mustAbs(t, fixture), layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := Checks(repo, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return findings
}

func TestValidRepos(t *testing.T) {
	for _, fixture := range []string{"marketplace", "multi", "single"} {
		t.Run(fixture, func(t *testing.T) {
			for _, f := range runChecks(t, fixture) {
				t.Errorf("unexpected finding: %s", f.Message)
			}
		})
	}
}

func TestBrokenRepo(t *testing.T) {
	findings := runChecks(t, "broken")
	got := make([]string, len(findings))
	for i, f := range findings {
		got[i] = f.Message
	}

	want := []string{
		"missing owner.name",
		"marketplace source 'plugins/oops' is not ./-prefixed",
		"marketplace source './plugins/ghost' does not resolve",
		"marketplaces disagree on plugins",
		"stray .claude-plugin/plugin.json",
		"plugins/oops: missing .codex-plugin/plugin.json (remove \"codex\" from checks.plugin_manifests to opt out)",
		"plugins/oops: hooks/ directory is forbidden (incompatible hooks schemas across the required plugin manifests: claude, codex)",
		"name 'wrong-name' != directory 'bad-skill'",
		"description missing a 'Use when/after/before' trigger phrase (checks.description_pattern)",
		"license 'MIT' is forbidden",
		"plugins/vers: version mismatch (claude=0.1.0 codex=0.2)",
		"plugins/vers: version '0.2' is not strict semver",
		"plugins/vers: no skills under skills/",
	}
	for _, substr := range want {
		if !containsSubstring(got, substr) {
			t.Errorf("missing finding containing %q\ngot:\n  %s", substr, strings.Join(got, "\n  "))
		}
	}
	if len(findings) != len(want) {
		t.Errorf("got %d findings, want %d:\n  %s", len(findings), len(want), strings.Join(got, "\n  "))
	}
}

// TestConfiguredLicense covers the opt-in path: with checks.license set,
// every skill must declare exactly that license — and a declared license
// stops being a finding.
func TestConfiguredLicense(t *testing.T) {
	cfg := DefaultCheckConfig()
	cfg.License = "MIT"

	findings := runChecksCfg(t, "single", cfg) // solo-skill declares no license
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "license must be MIT (got '')") {
		got := make([]string, len(findings))
		for i, f := range findings {
			got[i] = f.Message
		}
		t.Errorf("want exactly one missing-license finding, got:\n  %s", strings.Join(got, "\n  "))
	}

	for _, f := range runChecksCfg(t, "broken", cfg) { // bad-skill declares MIT
		if strings.Contains(f.Message, "license") {
			t.Errorf("unexpected license finding: %s", f.Message)
		}
	}
}

// TestPluginManifestsOptOut covers dropping a manifest from the required set:
// without "codex", the broken fixture's missing-codex finding disappears, and
// so does the hooks/ finding — a hooks/ directory only conflicts when both the
// Claude and Codex manifests are targeted.
func TestPluginManifestsOptOut(t *testing.T) {
	cfg := DefaultCheckConfig()
	cfg.PluginManifests = []string{"claude"}

	for _, f := range runChecksCfg(t, "broken", cfg) {
		if strings.Contains(f.Message, ".codex-plugin/plugin.json") {
			t.Errorf("codex manifest still required: %s", f.Message)
		}
		if strings.Contains(f.Message, "hooks/ directory is forbidden") {
			t.Errorf("hooks still forbidden without codex: %s", f.Message)
		}
	}
}

// TestPluginManifestsUnknown pins that an unrecognized manifest kind is a
// config error, not a silent no-op.
func TestPluginManifestsUnknown(t *testing.T) {
	cfg := DefaultCheckConfig()
	cfg.PluginManifests = []string{"claude", "windsurf"}

	repo, err := layout.Detect(mustAbs(t, "single"), layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Checks(repo, cfg); err == nil || !strings.Contains(err.Error(), "windsurf") {
		t.Errorf("want unknown-manifest error naming windsurf, got %v", err)
	}
}

func mustAbs(t *testing.T, fixture string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "e2e", "repos", fixture))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func containsSubstring(haystack []string, substr string) bool {
	for _, s := range haystack {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}
