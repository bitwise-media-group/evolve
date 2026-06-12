// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// New creates a temp workspace. dirs are workspace-relative skills locations
// (e.g. ".claude/skills") — pass the union across the selected providers so
// one workspace serves a whole model sweep. The returned cleanup removes the
// workspace; pass keep=true to leave it behind for debugging.
func New(prefix string, skillsDir string, dirs []string, fixtures map[string]string,
	keep bool) (string, func(), error) {
	ws, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		if !keep {
			_ = os.RemoveAll(ws)
		}
	}

	skills, err := skillDirs(skillsDir)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	for _, dir := range dirs {
		target := filepath.Join(ws, dir)
		if err := os.MkdirAll(target, 0o755); err != nil {
			cleanup()
			return "", nil, err
		}
		for _, skill := range skills {
			if err := os.Symlink(skill, filepath.Join(target, filepath.Base(skill))); err != nil {
				cleanup()
				return "", nil, err
			}
		}
	}

	for rel, content := range fixtures {
		if !filepath.IsLocal(rel) {
			cleanup()
			return "", nil, fmt.Errorf("fixture path %q escapes the workspace", rel)
		}
		path := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			cleanup()
			return "", nil, err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return ws, cleanup, nil
}

// skillDirs lists the absolute paths of every skill directory (one containing
// a SKILL.md) under skillsDir, sorted by name.
func skillDirs(skillsDir string) ([]string, error) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(skillsDir, e.Name())
		if info, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil && info.Mode().IsRegular() {
			abs, err := filepath.Abs(dir)
			if err != nil {
				return nil, err
			}
			out = append(out, abs)
		}
	}
	sort.Strings(out)
	return out, nil
}
