// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package web

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser opens url in the user's default browser without blocking. It is
// best-effort: a failure is returned for the caller to log, not fatal.
func OpenBrowser(url string) error {
	name, args := browserOpenCommand(url)
	if name == "" {
		return fmt.Errorf("opening a browser is unsupported on %s", runtime.GOOS)
	}
	return exec.Command(name, args...).Start()
}

// browserOpenCommand returns the platform command that opens url, or "" when the
// platform is unknown. Split out so it can be tested without spawning a process.
func browserOpenCommand(url string) (name string, args []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", []string{url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}
