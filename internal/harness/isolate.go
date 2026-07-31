// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package harness

import (
	"os"
	"path/filepath"
)

// This file holds the shared session-isolation helpers. Every harness points
// its CLI at a throwaway, workspace-rooted state directory (".evolve/<name>-home")
// so trigger/eval runs never touch the operator's real session history or
// long-term memory; the directory dies with the workspace. Auth is bridged in
// from the operator's real config root — symlinked for auth-only files (so a
// mid-run token refresh writes through) and copied for files that mix
// credentials with mutable config (so a run can never write back). CLIs with a
// dedicated config-dir variable (CLAUDE_CONFIG_DIR, CODEX_HOME, COPILOT_HOME,
// GROK_HOME) get that; the rest run under an overridden $HOME.
//
// All helpers are best-effort and never fail the run: a failure here still
// leaves the env override set, so the CLI creates its own tree (possibly
// unauthenticated), which surfaces as a runtime error on the case rather than
// an evolve crash.

// isolatedDir is the absolute workspace-rooted path for a slash-separated
// relative state dir like ".evolve/grok-home".
func isolatedDir(ws, rel string) string {
	if ws == "" {
		return filepath.FromSlash(rel)
	}
	return filepath.Join(ws, filepath.FromSlash(rel))
}

// operatorDir is the operator's real config root (the source of bridged auth),
// not the per-workspace isolated one. Honors envVar when the parent evolve
// process itself has it set (pass "" for CLIs without one); otherwise
// ~/<defaultRel>.
func operatorDir(envVar, defaultRel string) string {
	if envVar != "" {
		if d := os.Getenv(envVar); d != "" {
			return d
		}
	}
	userHome, err := os.UserHomeDir()
	if err != nil || userHome == "" {
		return ""
	}
	return filepath.Join(userHome, defaultRel)
}

// linkFile exposes src inside an isolated state dir. Prefers a symlink so
// mid-run writes (e.g. a token refresh) go through to the real file; falls
// back to a one-shot 0600 copy when symlink is unavailable. No-op when src is
// missing (CI with env-var auth only) or dst already exists.
func linkFile(src, dst string) {
	if _, err := os.Lstat(dst); err == nil {
		return
	}
	if _, err := os.Stat(src); err != nil {
		return
	}
	if err := os.Symlink(src, dst); err == nil {
		return
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	_ = os.WriteFile(dst, data, 0o600)
}

// copyFile0600 copies src into an isolated state dir with owner-only
// permissions. Used instead of linkFile for files that mix credentials with
// mutable config: the agent may rewrite them mid-run, and a symlink would let
// those writes land in the operator's real file. No-op when src is missing or
// dst already exists.
func copyFile0600(src, dst string) {
	if _, err := os.Lstat(dst); err == nil {
		return
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	_ = os.WriteFile(dst, data, 0o600)
}

// linkGitConfig bridges the operator's git configuration (~/.gitconfig and the
// XDG ~/.config/git tree) into a fake HOME so git identity and settings keep
// working inside HOME-overridden agent sessions.
func linkGitConfig(fakeHome string) {
	realHome, err := os.UserHomeDir()
	if err != nil || realHome == "" || sameFilePath(realHome, fakeHome) {
		return
	}
	linkFile(filepath.Join(realHome, ".gitconfig"), filepath.Join(fakeHome, ".gitconfig"))
	xdgGit := filepath.Join(realHome, ".config", "git")
	if _, err := os.Stat(xdgGit); err != nil {
		return
	}
	dstDir := filepath.Join(fakeHome, ".config")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return
	}
	dst := filepath.Join(dstDir, "git")
	if _, err := os.Lstat(dst); err == nil {
		return
	}
	_ = os.Symlink(xdgGit, dst)
}

// sameFilePath reports whether a and b name the same path after cleaning.
func sameFilePath(a, b string) bool {
	a, errA := filepath.Abs(a)
	b, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return a == b
}
