// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

// pluginManifests maps a checks.plugin_manifests kind to its manifest path,
// relative to the plugin directory.
var pluginManifests = map[string]string{
	"claude": filepath.Join(".claude-plugin", "plugin.json"),
	"codex":  filepath.Join(".codex-plugin", "plugin.json"),
}

// compatibleManifests reports whether the given plugin manifest kinds can
// coexist in one plugin. Claude and Codex both auto-discover hooks/hooks.json
// with mutually incompatible schemas, so a plugin cannot ship both — today
// that is the only conflict, so any other combination is compatible.
func compatibleManifests(kinds []string) bool {
	return !slices.Contains(kinds, "claude") || !slices.Contains(kinds, "codex")
}

// CheckConfig holds the tunable knobs, overridable via the .evolve config so other
// organizations can run the tool without forking the rules.
type CheckConfig struct {
	License             string   // required SKILL.md license; "" forbids the field
	TriggerPattern      string   // regex the description must match
	MaxSkillLines       int      // SKILL.md body cap
	MaxNameRunes        int      // skill name cap
	MaxDescriptionRunes int      // skill description cap
	PluginManifests     []string // plugin manifests every plugin must ship: "claude" and/or "codex"
	Marketplace         bool     // validate marketplace manifests (marketplace layout only)

	Signals SignalConfig // tunables for the non-blocking skill-quality signals
}

// DefaultCheckConfig mirrors the rules hard-coded in run_checks.py, except
// that the license requirement is opt-in: by default skills must not declare
// a license at all.
func DefaultCheckConfig() CheckConfig {
	return CheckConfig{
		License:             "",
		TriggerPattern:      `Use (when|after|before)`,
		MaxSkillLines:       500,
		MaxNameRunes:        64,
		MaxDescriptionRunes: 1024,
		PluginManifests:     []string{"claude", "codex"},
		Marketplace:         true,
		Signals:             DefaultSignalConfig(),
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
	known := slices.Sorted(maps.Keys(pluginManifests))
	for _, kind := range cfg.PluginManifests {
		if _, ok := pluginManifests[kind]; !ok {
			return nil, fmt.Errorf("checks.plugin_manifests: unknown manifest %q (want one of %s)",
				kind, strings.Join(known, ", "))
		}
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

	manifests := c.checkPluginManifests(dir, label, expectedName)

	// A hooks/ directory only conflicts when the required manifests are
	// incompatible: their agents discover hooks.json with clashing schemas.
	if !compatibleManifests(manifests) {
		if info, err := os.Stat(filepath.Join(dir, "hooks")); err == nil && info.IsDir() {
			c.errf("%s: hooks/ directory is forbidden (incompatible hooks schemas across the "+
				"required plugin manifests: %s)", label, strings.Join(manifests, ", "))
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

// loadedManifest is a present, parsed plugin manifest paired with the kind it
// was required as ("claude", "codex", …).
type loadedManifest struct {
	kind string
	path string // absolute, for repo-relative finding paths
	obj  map[string]any
}

// checkPluginManifests validates the plugin.json manifests configured in
// checks.plugin_manifests: presence, JSON shape, name agreement with the
// directory (or each other), and strict, agreeing semver. It returns the
// required manifest kinds so the caller can judge their compatibility.
// The set of manifests is driven entirely by the config list and
// the pluginManifests map, so adding a kind needs no changes here.
func (c *checker) checkPluginManifests(dir, label, expectedName string) (kinds []string) {
	// Sort the kinds so findings are stable regardless of config ordering.
	kinds = slices.Clone(c.cfg.PluginManifests)

	var present []loadedManifest
	for _, kind := range kinds {
		rel := pluginManifests[kind]
		pj := filepath.Join(dir, rel)
		if !isFile(pj) {
			c.errf("%s: missing %s (remove %q from checks.plugin_manifests to opt out)",
				label, filepath.ToSlash(rel), kind)
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
		present = append(present, loadedManifest{kind: kind, path: pj, obj: obj})
	}

	// Cross-manifest agreement only holds when every required manifest is present.
	if len(kinds) > 0 && len(present) == len(kinds) {
		c.checkManifestAgreement(label, expectedName, present)
	}

	return kinds
}

// checkManifestAgreement validates names and versions across the required
// manifests: each must name the plugin directory (or, for single-plugin repos,
// they must agree on a kebab-case name), and every version must be strict semver
// and agree with the others.
func (c *checker) checkManifestAgreement(label, expectedName string, present []loadedManifest) {
	if expectedName != "" {
		for _, m := range present {
			if name := jsonStr(m.obj["name"]); name != expectedName {
				c.errf("%s: name '%s' != directory '%s'", c.repo.Rel(m.path), name, expectedName)
			}
		}
	} else {
		names := map[string]string{}
		for _, m := range present {
			names[m.path] = jsonStr(m.obj["name"])
		}
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

	// Versions must agree across manifests and each be strict semver.
	versions := map[string]string{}
	pairs := make([]string, 0, len(present))
	for _, m := range present {
		v := jsonStr(m.obj["version"])
		versions[m.path] = v
		pairs = append(pairs, m.kind+"="+v)
		if !semverRE.MatchString(v) {
			c.errf("%s: version '%s' is not strict semver", label, v)
		}
	}
	if len(uniqueSorted(versions)) > 1 {
		c.errf("%s: version mismatch (%s)", label, strings.Join(pairs, " "))
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
		c.errf("%s: description missing a 'Use when/after/before' trigger phrase (checks.description_pattern)", path)
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
			c.errf("%s: SKILL.md exceeds %d lines (%d) (checks.max_skill_lines)", path, c.cfg.MaxSkillLines, lines)
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
			c.errf("missing %s (set checks.marketplace: false to opt out)", c.repo.Rel(mp))
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
