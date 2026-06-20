// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

//go:build darwin

package runner

import (
	"fmt"
	"os/exec"
	"strings"
)

// sandboxWrap confines the command with sandbox-exec (Seatbelt). The profile
// allows everything, then denies writes under each protected root, then
// re-permits the workspace; Seatbelt is last-match-wins, so the workspace
// allow overrides the deny when the workspace sits inside a protected root.
func sandboxWrap(workspace string, roots []string, argv []string) ([]string, error) {
	launcher, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return nil, fmt.Errorf(
			"sandbox enabled but sandbox-exec not found; set sandbox.enabled=false to run unconfined: %w", err)
	}
	return append([]string{launcher, "-p", seatbeltProfile(workspace, roots)}, argv...), nil
}

// seatbeltProfile renders the SBPL profile. Reads, exec, and the network are
// left unrestricted by (allow default); only writes under the protected roots
// are carved out, then the workspace is restored.
func seatbeltProfile(workspace string, roots []string) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n")
	for _, root := range roots {
		fmt.Fprintf(&b, "(deny file-write* (subpath %s))\n", sbplString(root))
	}
	if workspace != "" {
		fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", sbplString(workspace))
	}
	return b.String()
}

// sbplString renders s as an SBPL double-quoted string literal, escaping the
// two characters that are special inside one.
func sbplString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
