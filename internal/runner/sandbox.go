// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package runner

import "path/filepath"

// Sandbox confines an agent run's filesystem writes. When Enabled, every
// command is wrapped in an OS sandbox (sandbox-exec on macOS, bubblewrap on
// Linux) that permits writes everywhere EXCEPT ProtectedRoots — the source
// repositories an escaping agent must never modify — with the per-run
// workspace always re-permitted on top. Reads and the network stay
// unrestricted on purpose: dependency tooling (go mod download, npm ci, uv
// sync, terraform init, ...) writes to caches scattered across $HOME and needs
// the network, and those caches must keep working for tools we ship today and
// tools other teams add tomorrow. The policy is therefore a denylist (protect
// the repos) rather than an allowlist (enumerate every cache), so it never has
// to know about a tool in advance.
type Sandbox struct {
	Enabled        bool
	ProtectedRoots []string // dirs kept read-only to the agent (the workspace is always writable)
}

// wrap returns argv prefixed with the platform sandbox launcher confining
// writes to everything but the protected roots, with workspace re-permitted. A
// disabled sandbox returns argv unchanged. workspace is the directory the agent
// runs in and is always writable (it is re-permitted even when it sits inside a
// protected root). wrap fails closed: an enabled sandbox that cannot be
// constructed (missing helper binary, unsupported platform) returns an error
// rather than silently running the agent unconfined.
func (s Sandbox) wrap(workspace string, argv []string) ([]string, error) {
	if !s.Enabled {
		return argv, nil
	}
	return sandboxWrap(resolvePath(workspace), resolvePaths(s.ProtectedRoots), argv)
}

// resolvePath makes p absolute and resolves symlinks, so the rules match the
// canonical path the kernel enforces against — $TMPDIR on macOS is a symlink
// into /private/var/folders, and a rule written against the unresolved path
// would never match. An unresolvable path (e.g. not yet created) falls back to
// its absolute form.
func resolvePath(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

func resolvePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if r := resolvePath(p); r != "" {
			out = append(out, r)
		}
	}
	return out
}
