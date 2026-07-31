// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package workspace

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// New creates a temp workspace under parent (an empty parent uses the system
// temp dir, $TMPDIR). skills are absolute skill-directory paths to symlink in;
// the caller chooses which (triggers pass every skill via SkillDirs so the model
// must pick the right one; evals pass only the skill under test). dirs are
// workspace-relative skills locations (e.g. ".claude/skills") — pass the union
// across the selected providers so one workspace serves a whole model sweep.
// copies maps workspace-relative destinations to absolute fixture paths, copied
// in byte-for-byte. The workspace is then made a git repository with the whole
// fixture state as an initial commit. The returned cleanup removes the
// workspace; pass keep=true to leave it behind for debugging (or so a
// run-scoped parent owns its removal).
func New(parent, prefix string, skills []string, dirs []string, copies map[string]string,
	keep bool) (string, func(), error) {
	ws, err := os.MkdirTemp(parent, prefix)
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		if !keep {
			_ = os.RemoveAll(ws)
		}
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

	for rel, src := range copies {
		if !filepath.IsLocal(rel) {
			cleanup()
			return "", nil, fmt.Errorf("fixture path %q escapes the workspace", rel)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("fixture %s: %w", rel, err)
		}
		path := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			cleanup()
			return "", nil, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			cleanup()
			return "", nil, err
		}
	}

	if err := initRepo(ws); err != nil {
		cleanup()
		return "", nil, err
	}
	return ws, cleanup, nil
}

// initRepo makes the workspace a git repository with the fixture state as the
// initial commit: repo-root-detecting tools (actionlint, pre-commit, the codex
// CLI's git check) find a project, and the agent's changes are exactly the
// diff against HEAD. Git runs insulated from operator and system configuration
// — no signing, ignores, or identity leaks in — so workspace creation behaves
// the same on any machine, including CI with no git identity at all. This is a
// setup-time exec like the claude harness's Keychain bridge; agent execution
// still goes through internal/runner only.
func initRepo(ws string) error {
	env := append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
	)
	for _, args := range [][]string{
		{"-c", "init.defaultBranch=main", "init", "-q"},
		{"-c", "core.excludesFile=" + os.DevNull, "add", "-A"},
		{"-c", "user.name=evolve", "-c", "user.email=evolve@workspace.invalid",
			"commit", "-q", "--allow-empty", "-m", "initial workspace state"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = ws
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, bytes.TrimSpace(out))
		}
	}
	return nil
}

// SkillDirs lists the absolute paths of every skill directory (one containing
// a SKILL.md) under skillsDir, sorted by name.
func SkillDirs(skillsDir string) ([]string, error) {
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
