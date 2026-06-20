// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

//go:build linux

package runner

import (
	"fmt"
	"os/exec"
)

// sandboxWrap confines the command with bubblewrap. The host filesystem is
// bound read-write, each protected root is remounted read-only, and the
// workspace is bound read-write on top — later binds shadow earlier ones, so
// the workspace stays writable even inside a protected root. The network is
// shared (dependency tooling needs it) and the sandboxed process dies with the
// runner so a killed sweep leaves nothing behind.
func sandboxWrap(workspace string, roots []string, argv []string) ([]string, error) {
	launcher, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, fmt.Errorf("sandbox enabled but bwrap (bubblewrap) not found; install it or set sandbox.enabled=false: %w", err)
	}
	bw := []string{launcher, "--dev-bind", "/", "/", "--share-net", "--die-with-parent"}
	for _, root := range roots {
		bw = append(bw, "--ro-bind", root, root)
	}
	if workspace != "" {
		bw = append(bw, "--bind", workspace, workspace, "--chdir", workspace)
	}
	bw = append(bw, "--")
	return append(bw, argv...), nil
}
