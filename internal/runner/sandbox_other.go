// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

//go:build !darwin && !linux

package runner

import (
	"fmt"
	"runtime"
)

// sandboxWrap fails closed: there is no filesystem-confinement primitive wired
// up for this platform, so an enabled sandbox refuses to run rather than
// silently leaving the agent unconfined.
func sandboxWrap(_ string, _ []string, _ []string) ([]string, error) {
	return nil, fmt.Errorf("filesystem sandbox is not supported on %s; set sandbox.enabled=false to run unconfined", runtime.GOOS)
}
