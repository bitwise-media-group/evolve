// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

//go:build darwin

package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSeatbeltProfile(t *testing.T) {
	got := seatbeltProfile("/tmp/ws", []string{"/home/x/Repos", "/work/src"})
	want := "(version 1)\n" +
		"(allow default)\n" +
		`(deny file-write* (subpath "/home/x/Repos"))` + "\n" +
		`(deny file-write* (subpath "/work/src"))` + "\n" +
		`(allow file-write* (subpath "/tmp/ws"))` + "\n"
	if got != want {
		t.Fatalf("seatbeltProfile() =\n%q\nwant\n%q", got, want)
	}
}

func TestSeatbeltProfileOrderingRepermitsWorkspace(t *testing.T) {
	// The workspace allow must come after the deny so last-match-wins keeps a
	// workspace nested inside a protected root writable.
	got := seatbeltProfile("/home/x/Repos/ws", []string{"/home/x/Repos"})
	deny := strings.Index(got, "(deny file-write*")
	allow := strings.Index(got, `(allow file-write* (subpath "/home/x/Repos/ws"`)
	if deny < 0 || allow < 0 || allow < deny {
		t.Fatalf("workspace allow must follow the deny; got\n%s", got)
	}
}

func TestSbplStringEscapes(t *testing.T) {
	if got := sbplString(`/a/b"c\d`); got != `"/a/b\"c\\d"` {
		t.Fatalf("sbplString() = %s, want %s", got, `"/a/b\"c\\d"`)
	}
}

func TestSandboxWrapPrependsSandboxExec(t *testing.T) {
	argv := []string{"/usr/bin/claude", "-p", "hi"}
	got, err := Sandbox{Enabled: true, ProtectedRoots: []string{"/home/x/Repos"}}.wrap("/tmp/ws", argv)
	if err != nil {
		t.Fatalf("wrap() error = %v", err)
	}
	if len(got) < 4 || filepath.Base(got[0]) != "sandbox-exec" || got[1] != "-p" {
		t.Fatalf("wrap() = %v, want [sandbox-exec -p <profile> ...]", got)
	}
	if !slices.Equal(got[3:], argv) {
		t.Fatalf("wrap() tail = %v, want original argv %v", got[3:], argv)
	}
	if !strings.Contains(got[2], "(deny file-write*") {
		t.Fatalf("wrap() profile missing deny rule: %s", got[2])
	}
}

// TestSeatbeltEnforces runs sandbox-exec for real. It is skipped when the
// profile cannot be applied — most notably when the test itself runs inside
// another sandbox that forbids nesting (sandbox_apply: Operation not
// permitted), as in some CI. On a normal macOS host it proves the policy
// denies a write to a protected root while permitting the workspace.
func TestSeatbeltEnforces(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}
	base := t.TempDir()
	real, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(real, "Repos")
	ws := filepath.Join(repo, "ws") // deliberately nested inside the protected root
	for _, d := range []string{repo, ws} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	profile := seatbeltProfile(ws, []string{repo})

	write := func(path string) error {
		return exec.Command("sandbox-exec", "-p", profile, "/bin/sh", "-c", "echo x > "+path).Run()
	}
	// A probe write tells nesting-blocked (skip) apart from a real deny.
	if out, err := exec.Command("sandbox-exec", "-p", profile, "/bin/sh", "-c", "true").CombinedOutput(); err != nil {
		t.Skipf("sandbox-exec cannot apply a profile here (%v): %s", err, out)
	}

	if err := write(filepath.Join(repo, "blocked")); err == nil {
		t.Error("write to protected root succeeded, want denied")
	}
	if err := write(filepath.Join(ws, "allowed")); err != nil {
		t.Errorf("write to workspace failed: %v, want allowed", err)
	}
}
