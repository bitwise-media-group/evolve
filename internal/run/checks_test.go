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
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "repos", fixture))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := Checks(repo, DefaultCheckConfig())
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
		"plugins/oops: missing .codex-plugin/plugin.json",
		"plugins/oops: hooks/ directory is forbidden",
		"name 'wrong-name' != directory 'bad-skill'",
		"description missing a 'Use when/after/before' trigger phrase",
		"license must be MIT (got '')",
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

func containsSubstring(haystack []string, substr string) bool {
	for _, s := range haystack {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}
