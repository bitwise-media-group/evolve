// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"fmt"
	"os"
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

// TestTriggerPatternInvalid covers the trigger_pattern compile error: a
// malformed regex is a config error, not a finding (checks.go:82).
func TestTriggerPatternInvalid(t *testing.T) {
	cfg := DefaultCheckConfig()
	cfg.TriggerPattern = "("

	repo, err := layout.Detect(mustAbs(t, "single"), layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Checks(repo, cfg); err == nil || !strings.Contains(err.Error(), "checks.trigger_pattern") {
		t.Errorf("want trigger_pattern compile error, got %v", err)
	}
}

// TestSkillLengthLimits covers the description- and SKILL.md-length caps by
// pinning them low against the otherwise-clean single fixture
// (checks.go:313, checks.go:331).
func TestSkillLengthLimits(t *testing.T) {
	t.Run("description", func(t *testing.T) {
		cfg := DefaultCheckConfig()
		cfg.MaxDescriptionRunes = 10
		findings := runChecksCfg(t, "single", cfg)
		if !containsSubstring(messages(findings), "description longer than 10 chars") {
			t.Errorf("want over-length description finding, got:\n  %s",
				strings.Join(messages(findings), "\n  "))
		}
	})
	t.Run("skill-lines", func(t *testing.T) {
		cfg := DefaultCheckConfig()
		cfg.MaxSkillLines = 1
		findings := runChecksCfg(t, "single", cfg)
		if !containsSubstring(messages(findings), "SKILL.md exceeds 1 lines") {
			t.Errorf("want over-length SKILL.md finding, got:\n  %s",
				strings.Join(messages(findings), "\n  "))
		}
	})
}

// TestManifestNotObject covers a plugin.json whose top-level JSON is not an
// object (checks.go:227).
func TestManifestNotObject(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), "[]")
	writeFile(t, filepath.Join(root, "skills", "demo", "SKILL.md"), skillMD("demo"))

	cfg := DefaultCheckConfig()
	cfg.PluginManifests = []string{"claude"}

	findings := checkDir(t, root, cfg)
	wantOnly(t, findings, ".claude-plugin/plugin.json: manifest is not a JSON object")
}

// TestManifestNameVsDirectory covers a multi-plugin repo whose manifest name
// disagrees with the plugin directory (checks.go:249).
func TestManifestNameVsDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", "myplugin")
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"), pluginJSON("wrong", "0.2.0"))
	writeFile(t, filepath.Join(dir, ".codex-plugin", "plugin.json"), pluginJSON("wrong", "0.2.0"))
	writeFile(t, filepath.Join(dir, "skills", "demo", "SKILL.md"), skillMD("demo"))

	findings := checkDir(t, root, DefaultCheckConfig())
	// Both required manifests disagree with the directory, one finding each.
	if got := count(messages(findings), "name 'wrong' != directory 'myplugin'"); got != 2 {
		t.Errorf("want 2 name-vs-directory findings, got %d:\n  %s",
			got, strings.Join(messages(findings), "\n  "))
	}
}

// TestManifestsDisagreeOnName covers a single-plugin repo whose two manifests
// name the plugin differently (checks.go:259).
func TestManifestsDisagreeOnName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), pluginJSON("alpha", "0.1.0"))
	writeFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), pluginJSON("beta", "0.1.0"))
	writeFile(t, filepath.Join(root, "skills", "demo", "SKILL.md"), skillMD("demo"))

	findings := checkDir(t, root, DefaultCheckConfig())
	wantOnly(t, findings, "manifests disagree on plugin name")
}

// TestManifestNameNotKebab covers a single-plugin repo whose manifests agree
// on a non-kebab-case plugin name (checks.go:263).
func TestManifestNameNotKebab(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), pluginJSON("Bad_Name", "0.1.0"))
	writeFile(t, filepath.Join(root, ".codex-plugin", "plugin.json"), pluginJSON("Bad_Name", "0.1.0"))
	writeFile(t, filepath.Join(root, "skills", "demo", "SKILL.md"), skillMD("demo"))

	findings := checkDir(t, root, DefaultCheckConfig())
	wantOnly(t, findings, "plugin name 'Bad_Name' not kebab-case")
}

// TestMissingMarketplaceManifest covers a marketplace repo missing one of the
// two required marketplace manifests (checks.go:344).
func TestMissingMarketplaceManifest(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude-plugin", "marketplace.json"),
		`{"name":"mp","owner":{"name":"me"},"plugins":[{"name":"alpha","source":"./plugins/alpha"}]}`)
	dir := filepath.Join(root, "plugins", "alpha")
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"), pluginJSON("alpha", "0.1.0"))
	writeFile(t, filepath.Join(dir, ".codex-plugin", "plugin.json"), pluginJSON("alpha", "0.1.0"))
	writeFile(t, filepath.Join(dir, "skills", "alpha", "SKILL.md"), skillMD("alpha"))

	findings := checkDir(t, root, DefaultCheckConfig())
	// The Codex marketplace manifest (.agents/plugins/marketplace.json) is absent.
	wantOnly(t, findings, "missing .agents/plugins/marketplace.json (set checks.marketplace: false to opt out)")
}

func checkDir(t *testing.T, root string, cfg CheckConfig) []Finding {
	t.Helper()
	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := Checks(repo, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return findings
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func pluginJSON(name, version string) string {
	return fmt.Sprintf(`{"name":%q,"version":%q}`, name, version)
}

func skillMD(name string) string {
	return fmt.Sprintf("---\nname: %[1]s\ndescription: Demo skill. Use when testing.\n---\n\n# %[1]s\n\nBody.\n", name)
}

func messages(findings []Finding) []string {
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = f.Message
	}
	return out
}

// wantOnly asserts the findings are exactly one message containing substr.
func wantOnly(t *testing.T, findings []Finding, substr string) {
	t.Helper()
	if len(findings) != 1 || !strings.Contains(findings[0].Message, substr) {
		t.Errorf("want exactly one finding containing %q, got:\n  %s",
			substr, strings.Join(messages(findings), "\n  "))
	}
}

func count(haystack []string, substr string) int {
	n := 0
	for _, s := range haystack {
		if strings.Contains(s, substr) {
			n++
		}
	}
	return n
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
