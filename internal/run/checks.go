// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/manifest"
)

var (
	nameRE   = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	semverRE = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
)

// CheckConfig holds the tunable knobs, overridable via the .evolve config so other
// organizations can run the tool without forking the rules.
type CheckConfig struct {
	License              string // required SKILL.md license; "" forbids the field
	TriggerPattern       string // regex the description must match
	MaxSkillLines        int    // SKILL.md body cap
	MaxNameRunes         int    // skill name cap
	MaxDescriptionRunes  int    // skill description cap
	RequireCodexManifest bool   // require .codex-plugin/plugin.json beside Claude's
	ForbidHooks          bool   // forbid a hooks/ directory in plugins
	Marketplace          bool   // validate marketplace manifests (marketplace layout only)

	Signals SignalConfig // tunables for the non-blocking skill-quality signals
}

// DefaultCheckConfig mirrors the rules hard-coded in run_checks.py, except
// that the license requirement is opt-in: by default skills must not declare
// a license at all.
func DefaultCheckConfig() CheckConfig {
	return CheckConfig{
		License:              "",
		TriggerPattern:       `Use (when|after|before)`,
		MaxSkillLines:        500,
		MaxNameRunes:         64,
		MaxDescriptionRunes:  1024,
		RequireCodexManifest: true,
		ForbidHooks:          true,
		Marketplace:          true,
		Signals:              DefaultSignalConfig(),
	}
}

// Finding is one failed check.
type Finding struct {
	Message string
}

// Checks executes every check layer appropriate for the repository's layout
// and returns the findings in emission order.
func Checks(repo *layout.Repo, cfg CheckConfig) ([]Finding, error) {
	triggerRE, err := regexp.Compile(cfg.TriggerPattern)
	if err != nil {
		return nil, fmt.Errorf("checks.trigger_pattern: %w", err)
	}
	c := &checker{repo: repo, cfg: cfg, triggerRE: triggerRE}

	switch repo.Kind {
	case layout.Single:
		c.checkPlugin(repo.Root, "")
	default:
		if repo.Kind == layout.Marketplace {
			if cfg.Marketplace {
				c.checkMarketplace()
			}
			if isFile(filepath.Join(repo.Root, ".claude-plugin", "plugin.json")) {
				c.errf("repo root: stray .claude-plugin/plugin.json in a marketplace repository")
			}
		}
		if len(repo.Plugins) == 0 {
			c.errf("no plugins under plugins/ (for a root-level plugin repo, use the single layout)")
		}
		for _, p := range repo.Plugins {
			c.checkPlugin(p.Dir, p.Name)
		}
	}
	c.checkEvalSpecs()
	return c.findings, nil
}

// checkEvalSpecs validates the authored eval definitions — a layer the Python
// harness never had.
func (c *checker) checkEvalSpecs() {
	sets, err := c.repo.EvalSets()
	if err != nil {
		c.errf("enumerating evals: %v", err)
		return
	}
	for _, set := range sets {
		if set.TriggersPath != "" {
			spec, err := evalspec.LoadTriggers(set.TriggersPath)
			if err != nil {
				c.errf("%s: %v", c.repo.Rel(set.TriggersPath), err)
			} else {
				for _, problem := range evalspec.ValidateTriggers(spec.Triggers) {
					c.errf("%s: %s", c.repo.Rel(set.TriggersPath), problem)
				}
			}
		}
		if set.EvalsPath != "" {
			spec, err := evalspec.LoadEvals(set.EvalsPath)
			if err != nil {
				c.errf("%s: %v", c.repo.Rel(set.EvalsPath), err)
			} else {
				for _, problem := range evalspec.ValidateEvals(spec.Evals) {
					c.errf("%s: %s", c.repo.Rel(set.EvalsPath), problem)
				}
			}
		}
		if !isDir(set.SkillDir) {
			c.errf("%s: evals/%s has no matching skill at %s",
				c.repo.Rel(set.Plugin.Dir), set.Skill, c.repo.Rel(set.SkillDir))
		}
	}
}

type checker struct {
	repo      *layout.Repo
	cfg       CheckConfig
	triggerRE *regexp.Regexp
	findings  []Finding
}

func (c *checker) errf(format string, args ...any) {
	c.findings = append(c.findings, Finding{Message: fmt.Sprintf(format, args...)})
}

// checkPlugin checks one plugin rooted at dir. expectedName pins the manifest
// name to the plugin directory; "" (single-plugin repos) skips that, since a
// checkout directory name is arbitrary, and requires the manifests to agree.
func (c *checker) checkPlugin(dir, expectedName string) {
	label := "repo root"
	if dir != c.repo.Root {
		label = c.repo.Rel(dir)
	}
	claudePJ := filepath.Join(dir, ".claude-plugin", "plugin.json")
	codexPJ := filepath.Join(dir, ".codex-plugin", "plugin.json")

	required := []string{claudePJ}
	if c.cfg.RequireCodexManifest {
		required = append(required, codexPJ)
	}
	manifests := map[string]map[string]any{}
	for _, pj := range required {
		if !isFile(pj) {
			rel, _ := filepath.Rel(dir, pj)
			c.errf("%s: missing %s", label, filepath.ToSlash(rel))
			continue
		}
		v, err := manifest.ReadJSON(pj)
		if err != nil {
			c.errf("%s: %v", c.repo.Rel(pj), err)
			continue
		}
		obj, ok := v.(map[string]any)
		if !ok {
			c.errf("%s: manifest is not a JSON object", c.repo.Rel(pj))
			continue
		}
		manifests[pj] = obj
	}

	if len(manifests) == len(required) {
		names := map[string]string{}
		for pj, obj := range manifests {
			names[pj] = jsonStr(obj["name"])
		}
		if expectedName != "" {
			for _, pj := range required {
				if name := names[pj]; name != expectedName {
					c.errf("%s: name '%s' != directory '%s'", c.repo.Rel(pj), name, expectedName)
				}
			}
		} else {
			unique := uniqueSorted(names)
			if len(unique) > 1 {
				c.errf("%s: manifests disagree on plugin name: %v", label, unique)
			}
			for _, name := range unique {
				if !nameRE.MatchString(name) {
					c.errf("%s: plugin name '%s' not kebab-case", label, name)
				}
			}
		}

		// Versions must agree across manifests and be strict semver. With a
		// single required manifest the semver check applies to it directly.
		version := jsonStr(manifests[required[len(required)-1]]["version"])
		if len(required) == 2 {
			claudeVer := jsonStr(manifests[claudePJ]["version"])
			if claudeVer != version {
				c.errf("%s: version mismatch (claude=%s codex=%s)", label, claudeVer, version)
			}
		}
		if !semverRE.MatchString(version) {
			c.errf("%s: version '%s' is not strict semver", label, version)
		}
	}

	// Codex default-discovers hooks/hooks.json with a non-Claude schema.
	if c.cfg.ForbidHooks {
		if info, err := os.Stat(filepath.Join(dir, "hooks")); err == nil && info.IsDir() {
			c.errf("%s: hooks/ directory is forbidden (Codex discovers hooks.json with an incompatible schema)", label)
		}
	}

	skills, _ := filepath.Glob(filepath.Join(dir, "skills", "*", "SKILL.md"))
	sort.Strings(skills)
	if len(skills) == 0 {
		c.errf("%s: no skills under skills/", label)
	}
	for _, skillMD := range skills {
		c.checkSkill(skillMD)
	}
}

func (c *checker) checkSkill(skillMD string) {
	path := c.repo.Rel(skillMD)
	fields, ok, err := manifest.Frontmatter(skillMD)
	if err != nil {
		c.errf("%s: unreadable (%v)", path, err)
		return
	}
	if !ok {
		c.errf("%s: no YAML frontmatter", path)
		return
	}
	name := fields["name"]
	description := fields["description"]
	directory := filepath.Base(filepath.Dir(skillMD))

	if name != directory {
		c.errf("%s: name '%s' != directory '%s'", path, name, directory)
	}
	if !nameRE.MatchString(name) {
		c.errf("%s: name '%s' not kebab-case", path, name)
	}
	if utf8.RuneCountInString(name) > c.cfg.MaxNameRunes {
		c.errf("%s: name longer than %d chars", path, c.cfg.MaxNameRunes)
	}

	if description == "" {
		c.errf("%s: empty description", path)
	}
	if utf8.RuneCountInString(description) > c.cfg.MaxDescriptionRunes {
		c.errf("%s: description longer than %d chars", path, c.cfg.MaxDescriptionRunes)
	}
	if !c.triggerRE.MatchString(description) {
		c.errf("%s: description missing a 'Use when/after/before' trigger phrase", path)
	}

	got, present := fields["license"]
	switch {
	case c.cfg.License == "":
		if present {
			c.errf("%s: license '%s' is forbidden (no checks.license configured)", path, got)
		}
	case got != c.cfg.License:
		c.errf("%s: license must be %s (got '%s')", path, c.cfg.License, got)
	}

	if data, err := os.ReadFile(skillMD); err == nil {
		if lines := len(manifest.SplitLines(string(data))); lines > c.cfg.MaxSkillLines {
			c.errf("%s: SKILL.md exceeds %d lines (%d)", path, c.cfg.MaxSkillLines, lines)
		}
	}
}

func (c *checker) checkMarketplace() {
	claudeMP := filepath.Join(c.repo.Root, ".claude-plugin", "marketplace.json")
	codexMP := filepath.Join(c.repo.Root, ".agents", "plugins", "marketplace.json")

	markets := map[string]map[string]any{}
	ordered := []string{claudeMP, codexMP}
	for _, mp := range ordered {
		if !isFile(mp) {
			c.errf("missing %s", c.repo.Rel(mp))
			continue
		}
		v, err := manifest.ReadJSON(mp)
		if err != nil {
			c.errf("%s: %v", c.repo.Rel(mp), err)
			continue
		}
		obj, ok := v.(map[string]any)
		if !ok {
			c.errf("%s: manifest is not a JSON object", c.repo.Rel(mp))
			continue
		}
		markets[mp] = obj
		plugins, isList := obj["plugins"].([]any)
		if jsonStr(obj["name"]) == "" || !isList || len(plugins) == 0 {
			c.errf("%s: missing name or non-empty plugins array", c.repo.Rel(mp))
		}
	}

	if obj, ok := markets[claudeMP]; ok {
		owner, _ := obj["owner"].(map[string]any)
		if jsonStr(owner["name"]) == "" {
			c.errf("%s: missing owner.name", c.repo.Rel(claudeMP))
		}
	}

	// Every marketplace source must be ./-prefixed and resolve to a plugin
	// directory (Codex fallback-reads Claude's manifest against the repo root).
	for _, mp := range ordered {
		obj, ok := markets[mp]
		if !ok {
			continue
		}
		plugins, _ := obj["plugins"].([]any)
		for _, entry := range plugins {
			plugin, _ := entry.(map[string]any)
			source := plugin["source"]
			if obj, ok := source.(map[string]any); ok {
				source = obj["path"]
			}
			src := jsonStr(source)
			if !strings.HasPrefix(src, "./") {
				c.errf("marketplace source '%s' is not ./-prefixed", src)
			} else if info, err := os.Stat(filepath.Join(c.repo.Root, src)); err != nil || !info.IsDir() {
				c.errf("marketplace source '%s' does not resolve", src)
			}
		}
	}

	if len(markets) == 2 {
		claudeNames := pluginNames(markets[claudeMP])
		codexNames := pluginNames(markets[codexMP])
		if !equalStrings(claudeNames, codexNames) {
			c.errf("marketplaces disagree on plugins: claude=%v codex=%v", claudeNames, codexNames)
		}
	}
}

func pluginNames(market map[string]any) []string {
	plugins, _ := market["plugins"].([]any)
	names := make([]string, 0, len(plugins))
	for _, entry := range plugins {
		plugin, _ := entry.(map[string]any)
		names = append(names, jsonStr(plugin["name"]))
	}
	sort.Strings(names)
	return names
}

// jsonStr coerces a decoded JSON value to a string the way Python's str()
// does for the manifest fields the checks read; nil becomes "".
func jsonStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func uniqueSorted(m map[string]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range m {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
