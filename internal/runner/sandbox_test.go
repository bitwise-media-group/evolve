// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package runner

import (
	"slices"
	"testing"
)

func TestSandboxDisabledIsPassthrough(t *testing.T) {
	argv := []string{"/usr/bin/claude", "-p", "hi"}
	got, err := Sandbox{Enabled: false, ProtectedRoots: []string{"/home/x/Repos"}}.wrap("/tmp/ws", argv)
	if err != nil {
		t.Fatalf("wrap() error = %v, want nil", err)
	}
	if !slices.Equal(got, argv) {
		t.Fatalf("wrap() = %v, want unchanged %v", got, argv)
	}
}

func TestResolvePathDropsEmpties(t *testing.T) {
	if got := resolvePaths([]string{"", ""}); len(got) != 0 {
		t.Fatalf("resolvePaths(empties) = %v, want none", got)
	}
}
