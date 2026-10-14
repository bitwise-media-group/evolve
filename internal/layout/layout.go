// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package layout

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/bitwise-media-group/evolve/internal/encfmt"
	"github.com/bitwise-media-group/evolve/internal/manifest"
)

// Kind names one of the supported repository shapes.
type Kind string

const (
	Auto        Kind = ""            // detect from markers
	Marketplace Kind = "marketplace" // marketplace manifest + plugins/ tree
	Multi       Kind = "multi"       // plugins/ tree without a marketplace
	Single      Kind = "single"      // repo root is the plugin
	Agents      Kind = "agents"      // agents/<name>/evals.<ext>: one hosted agent per directory
	Agent       Kind = "agent"       // agent/evals.<ext>: the repository is one hosted agent
)

// AgentsGroup is the synthetic plugin name every agent eval set is grouped
// under, so the planner, TUI, report, and viewer get their plugin slot
// without an agent-specific rename.
const AgentsGroup = "agents"

// ParseKind validates a --layout flag value.
func ParseKind(s string) (Kind, error) {
	switch Kind(s) {
	case Marketplace, Multi, Single, Agents, Agent:
		return Kind(s), nil
	case Auto, "auto":
		return Auto, nil
	}
	return Auto, fmt.Errorf("unknown layout %q (want auto, marketplace, multi, single, agents, or agent)", s)
}

// IsAgentKind reports whether k is one of the hosted-agent layouts.
func (k Kind) IsAgentKind() bool { return k == Agents || k == Agent }

// Plugin is one plugin within a repository.
type Plugin struct {
	Name      string // directory name; manifest name for single-plugin repos
	Dir       string // absolute; == Repo.Root for single-plugin repos
	SkillsDir string // Dir/skills
	EvalsDir  string // Dir/evals
}

// HostedAgent is one hosted agent: a directory holding its declarative
// definition (agent code, system prompt, ...) and its evals file.
type HostedAgent struct {
	Name string // directory name; agent.name (or the repo root's basename) for the agent layout
	Dir  string // absolute; holds evals.<ext> and results.<ext>
}

// Repo is a detected repository. A repository holds plugins or hosted agents,
// never both: Plugins is empty for the agent layouts and Agents for the rest.
type Repo struct {
	Root    string
	Kind    Kind
	Plugins []Plugin
	Agents  []HostedAgent
}

// IsAgentRepo reports whether the repository holds hosted agents.
func (r *Repo) IsAgentRepo() bool { return r.Kind.IsAgentKind() }

// EvalSet is one skill's eval definitions (and where its results persist).
// For a hosted agent (Agent set) the agent stands in for the skill: Plugin is
// the synthetic AgentsGroup rooted at the repository, Skill the agent name,
// and SkillDir and ResultsDir the agent's directory; there is never a
// triggers file.
type EvalSet struct {
	Plugin       Plugin
	Skill        string
	SkillDir     string // Plugin.SkillsDir/Skill (may not exist; checks flag that)
	TriggersPath string // "" when the skill has no triggers.<ext>
	EvalsPath    string // "" when the skill has no evals.<ext>
	ResultsDir   string // evals/<skill>, where results.<ext> persists
	Agent        bool   // a hosted agent's suite rather than a skill's
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
			return nil, fmt.Errorf("%s is not a recognized plugin or agent repository "+
				"(no .claude-plugin/marketplace.json, plugins/*/.claude-plugin/plugin.json, .claude-plugin/plugin.json, "+
				"agents/*/evals.<ext>, or agent/evals.<ext>)", abs)
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
			return nil, fmt.Errorf("no plugin or agent repository found walking up from the working directory; " +
				"recognized layouts: marketplace (.claude-plugin/marketplace.json), " +
				"multi (plugins/*/.claude-plugin/plugin.json), single (.claude-plugin/plugin.json), " +
				"agents (agents/*/evals.<ext>), agent (agent/evals.<ext>)")
		}
		dir = parent
	}
}

// classify reports the layout of dir, or Auto when dir matches none. When
// forced is not Auto only that layout's marker is consulted, so a forced walk
// stops at the nearest directory shaped like the requested layout. The agent
// markers rank below every plugin marker, so a single-layout plugin whose
// subagents live in an agents/ folder (no evals file there) still detects as
// a plugin.
func classify(dir string, forced Kind) Kind {
	markers := []struct {
		kind Kind
		ok   func() bool
	}{
		{Marketplace, func() bool { return isFile(filepath.Join(dir, ".claude-plugin", "marketplace.json")) }},
		{Multi, func() bool {
			manifests, _ := filepath.Glob(filepath.Join(dir, "plugins", "*", ".claude-plugin", "plugin.json"))
			return len(manifests) > 0
		}},
		{Single, func() bool { return isFile(filepath.Join(dir, ".claude-plugin", "plugin.json")) }},
		{Agents, func() bool { return len(agentDirs(dir)) > 0 }},
		{Agent, func() bool { return evalsFile(filepath.Join(dir, "agent")) != "" }},
	}
	for _, m := range markers {
		if (forced == Auto || forced == m.kind) && m.ok() {
			return m.kind
		}
	}
	return Auto
}

// agentDirs lists the agents/<name>/ directories that hold an evals file,
// sorted by name.
func agentDirs(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, "agents"))
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		dir := filepath.Join(root, "agents", e.Name())
		if e.IsDir() && evalsFile(dir) != "" {
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	return dirs
}

// evalsFile returns the first evals.<ext> in dir, or "" when there is none
// (an ambiguous pair still counts as present; EvalSets reports it).
func evalsFile(dir string) string {
	for _, ext := range encfmt.Extensions {
		if path := filepath.Join(dir, "evals."+ext); isFile(path) {
			return path
		}
	}
	return ""
}

// agentName reads agent.name from an agent evals file with a minimal decode,
// the way manifest.PluginName reads a plugin manifest; "" when unset or
// unreadable.
func agentName(evalsPath string) string {
	var f struct {
		Agent struct {
			Name string `json:"name"`
		} `json:"agent"`
	}
	if encfmt.DecodeFile(evalsPath, &f) != nil {
		return ""
	}
	return f.Agent.Name
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
	case Agents:
		for _, dir := range agentDirs(root) {
			repo.Agents = append(repo.Agents, HostedAgent{Name: filepath.Base(dir), Dir: dir})
		}
	case Agent:
		dir := filepath.Join(root, "agent")
		name := agentName(evalsFile(dir))
		if name == "" {
			name = filepath.Base(root)
		}
		repo.Agents = []HostedAgent{{Name: name, Dir: dir}}
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
// evals, across all plugins, in (plugin, skill) order. In an agent repository
// it yields one set per agent instead, in name order, all grouped under the
// synthetic AgentsGroup plugin rooted at the repository.
func (r *Repo) EvalSets() ([]EvalSet, error) {
	if r.IsAgentRepo() {
		return r.agentSets()
	}
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
				Plugin:     p,
				Skill:      e.Name(),
				SkillDir:   filepath.Join(p.SkillsDir, e.Name()),
				ResultsDir: dir,
			}
			if set.TriggersPath, err = encfmt.FindOne(dir, "triggers"); err != nil {
				return nil, err
			}
			if set.EvalsPath, err = encfmt.FindOne(dir, "evals"); err != nil {
				return nil, err
			}
			if set.TriggersPath != "" || set.EvalsPath != "" {
				sets = append(sets, set)
			}
		}
	}
	return sets, nil
}

// agentSets is EvalSets for an agent repository.
func (r *Repo) agentSets() ([]EvalSet, error) {
	group := Plugin{Name: AgentsGroup, Dir: r.Root}
	sets := make([]EvalSet, 0, len(r.Agents))
	for _, a := range r.Agents {
		path, err := encfmt.FindOne(a.Dir, "evals")
		if err != nil {
			return nil, err
		}
		if path == "" {
			continue
		}
		sets = append(sets, EvalSet{
			Plugin: group, Skill: a.Name, SkillDir: a.Dir, EvalsPath: path, ResultsDir: a.Dir, Agent: true,
		})
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
