// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package layout

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/bitwise-media-group/evolve/internal/manifest"
)

// Kind names one of the supported repository shapes.
type Kind string

const (
	Auto        Kind = ""            // detect from markers
	Marketplace Kind = "marketplace" // marketplace manifest + plugins/ tree
	Multi       Kind = "multi"       // plugins/ tree without a marketplace
	Single      Kind = "single"      // repo root is the plugin
)

// ParseKind validates a --layout flag value.
func ParseKind(s string) (Kind, error) {
	switch Kind(s) {
	case Marketplace, Multi, Single:
		return Kind(s), nil
	case Auto, "auto":
		return Auto, nil
	}
	return Auto, fmt.Errorf("unknown layout %q (want auto, marketplace, multi, or single)", s)
}

// Plugin is one plugin within a repository.
type Plugin struct {
	Name      string // directory name; manifest name for single-plugin repos
	Dir       string // absolute; == Repo.Root for single-plugin repos
	SkillsDir string // Dir/skills
	EvalsDir  string // Dir/evals
}

// Repo is a detected repository.
type Repo struct {
	Root    string
	Kind    Kind
	Plugins []Plugin
}

// EvalSet is one skill's eval definitions (and where its results persist).
type EvalSet struct {
	Plugin       Plugin
	Skill        string
	SkillDir     string // Plugin.SkillsDir/Skill (may not exist; checks flag that)
	TriggersPath string // "" when the skill has no triggers.json
	CasesPath    string // "" when the skill has no cases.json
	ResultsPath  string // evals/<skill>/results.json (created on first run)
}

// Detect resolves root (or walks up from the working directory when root is
// empty) to a repository of the forced kind, or of whichever kind its markers
// indicate when forced is Auto.
func Detect(root string, forced Kind) (*Repo, error) {
	if root != "" {
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("--root %s is not a directory", root)
		}
		kind := classify(abs, forced)
		if kind == Auto {
			return nil, fmt.Errorf("%s is not a recognized plugin repository "+
				"(no .claude-plugin/marketplace.json, plugins/*/.claude-plugin/plugin.json, or .claude-plugin/plugin.json)", abs)
		}
		return build(abs, kind)
	}

	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		if kind := classify(dir, forced); kind != Auto {
			return build(dir, kind)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("no plugin repository found walking up from the working directory; " +
				"recognized layouts: marketplace (.claude-plugin/marketplace.json), " +
				"multi (plugins/*/.claude-plugin/plugin.json), single (.claude-plugin/plugin.json)")
		}
		dir = parent
	}
}

// classify reports the layout of dir, or Auto when dir matches none. When
// forced is not Auto only that layout's marker is consulted, so a forced walk
// stops at the nearest directory shaped like the requested layout.
func classify(dir string, forced Kind) Kind {
	isMarketplace := isFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	manifests, _ := filepath.Glob(filepath.Join(dir, "plugins", "*", ".claude-plugin", "plugin.json"))
	isMulti := len(manifests) > 0
	isSingle := isFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))

	switch forced {
	case Marketplace:
		if isMarketplace {
			return Marketplace
		}
	case Multi:
		if isMulti {
			return Multi
		}
	case Single:
		if isSingle {
			return Single
		}
	default:
		switch {
		case isMarketplace:
			return Marketplace
		case isMulti:
			return Multi
		case isSingle:
			return Single
		}
	}
	return Auto
}

func build(root string, kind Kind) (*Repo, error) {
	repo := &Repo{Root: root, Kind: kind}
	switch kind {
	case Single:
		name := manifest.PluginName(root)
		if name == "" {
			name = filepath.Base(root)
		}
		repo.Plugins = []Plugin{newPlugin(name, root)}
	default:
		entries, err := os.ReadDir(filepath.Join(root, "plugins"))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root, "plugins", e.Name())
			repo.Plugins = append(repo.Plugins, newPlugin(e.Name(), dir))
		}
		sort.Slice(repo.Plugins, func(i, j int) bool { return repo.Plugins[i].Name < repo.Plugins[j].Name })
	}
	return repo, nil
}

func newPlugin(name, dir string) Plugin {
	return Plugin{
		Name:      name,
		Dir:       dir,
		SkillsDir: filepath.Join(dir, "skills"),
		EvalsDir:  filepath.Join(dir, "evals"),
	}
}

// EvalSets enumerates every evals/<skill>/ directory that defines triggers or
// cases, across all plugins, in (plugin, skill) order.
func (r *Repo) EvalSets() ([]EvalSet, error) {
	var sets []EvalSet
	for _, p := range r.Plugins {
		entries, err := os.ReadDir(p.EvalsDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(p.EvalsDir, e.Name())
			set := EvalSet{
				Plugin:      p,
				Skill:       e.Name(),
				SkillDir:    filepath.Join(p.SkillsDir, e.Name()),
				ResultsPath: filepath.Join(dir, "results.json"),
			}
			if path := filepath.Join(dir, "triggers.json"); isFile(path) {
				set.TriggersPath = path
			}
			if path := filepath.Join(dir, "cases.json"); isFile(path) {
				set.CasesPath = path
			}
			if set.TriggersPath != "" || set.CasesPath != "" {
				sets = append(sets, set)
			}
		}
	}
	return sets, nil
}

// Rel renders path relative to the repository root for messages, falling back
// to the absolute path when it is outside the root.
func (r *Repo) Rel(path string) string {
	rel, err := filepath.Rel(r.Root, path)
	if err != nil || rel == ".." || len(rel) > 1 && rel[:3] == ".."+string(filepath.Separator) {
		return path
	}
	return filepath.ToSlash(rel)
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
