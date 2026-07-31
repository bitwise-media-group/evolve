// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mkSkills creates name/SKILL.md under root for each name and returns their
// absolute paths.
func mkSkills(t *testing.T, root string, names ...string) []string {
	t.Helper()
	var dirs []string
	for _, name := range names {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

func linkedNames(t *testing.T, ws, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(ws, dir))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// SkillDirs lists every skill (a dir with a SKILL.md), sorted, and skips
// non-skill directories.
func TestSkillDirs(t *testing.T) {
	root := t.TempDir()
	mkSkills(t, root, "beta", "alpha", "gamma")
	if err := os.MkdirAll(filepath.Join(root, "not-a-skill"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := SkillDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "alpha"),
		filepath.Join(root, "beta"),
		filepath.Join(root, "gamma"),
	}
	if len(got) != len(want) {
		t.Fatalf("SkillDirs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SkillDirs[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// New links exactly the skills it is given: evals pass only the skill under
// test; triggers pass every skill via SkillDirs.
func TestNewLinksOnlyGivenSkills(t *testing.T) {
	root := t.TempDir()
	all := mkSkills(t, root, "one", "two", "three")
	const dir = ".claude/skills"

	ws, cleanup, err := New("", "evals.", []string{filepath.Join(root, "two")}, []string{dir}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if got := linkedNames(t, ws, dir); len(got) != 1 || got[0] != "two" {
		t.Fatalf("eval workspace linked %v, want only [two]", got)
	}

	wsAll, cleanupAll, err := New("", "triggers.", all, []string{dir}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupAll()
	if got := linkedNames(t, wsAll, dir); len(got) != len(all) {
		t.Fatalf("trigger workspace linked %v, want all %d", got, len(all))
	}
}

// git runs a git command in ws with operator/system config nulled out, so the
// assertions see the same repository state initRepo produced.
func git(t *testing.T, ws string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = ws
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// New leaves the workspace a git repository with the fixture state committed
// and nothing untracked, so repo-root-detecting tools find a project and the
// agent's changes are exactly the diff against HEAD.
func TestNewInitialisesGitRepo(t *testing.T) {
	root := t.TempDir()
	skills := mkSkills(t, root, "one")
	fixture := filepath.Join(root, "fixture.txt")
	if err := os.WriteFile(fixture, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, cleanup, err := New("", "evals.", skills, []string{".claude/skills"},
		map[string]string{"docs/fixture.txt": fixture}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if status := git(t, ws, "status", "--porcelain"); status != "" {
		t.Errorf("workspace not clean after New:\n%s", status)
	}
	if count := git(t, ws, "rev-list", "--count", "HEAD"); count != "1" {
		t.Errorf("commit count = %s, want 1", count)
	}
	if branch := git(t, ws, "branch", "--show-current"); branch != "main" {
		t.Errorf("branch = %s, want main", branch)
	}
}
