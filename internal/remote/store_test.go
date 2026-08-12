// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The basic save/load/delete round trip (with key normalization) lives in
// login_test.go's TestStoreRoundTrip; these cover the store's remaining
// guarantees.

func testStore(t *testing.T) *Store {
	t.Helper()
	return NewStoreAt(filepath.Join(t.TempDir(), "evolve", "credentials.json"))
}

func TestStorePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX permissions")
	}
	s := testStore(t)
	if err := s.Save("https://a.example.com", &Credential{IDToken: "a"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(s.path))
	if err != nil {
		t.Fatalf("stat credentials dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("credentials dir mode = %o, want 700", perm)
	}
	if info, err = os.Stat(s.path); err != nil {
		t.Fatalf("stat credentials file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credentials file mode = %o, want 600 (these are secrets)", perm)
	}
}

func TestStoreKeepsOtherRemotes(t *testing.T) {
	s := testStore(t)
	if err := s.Save("https://a.example.com", &Credential{IDToken: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("https://b.example.com", &Credential{IDToken: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("https://a.example.com"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Load("https://a.example.com"); !errors.Is(err, ErrLoginExpired) {
		t.Errorf("Load(deleted) = %v, want ErrLoginExpired", err)
	}
	if got, err := s.Load("https://b.example.com"); err != nil || got.IDToken != "b" {
		t.Errorf("Load(other remote) = (%+v, %v), the sibling must survive a delete", got, err)
	}
	// Deleting an absent remote is fine.
	if err := s.Delete("https://never-stored.example.com"); err != nil {
		t.Errorf("Delete(absent) = %v, want nil", err)
	}
}

func TestStoreEmptyIDTokenIsExpired(t *testing.T) {
	s := testStore(t)
	if err := s.Save("https://a.example.com", &Credential{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("https://a.example.com"); !errors.Is(err, ErrLoginExpired) {
		t.Errorf("Load(tokenless credential) = %v, want ErrLoginExpired", err)
	}
}

func TestStoreCorruptFile(t *testing.T) {
	s := testStore(t)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := s.Load("https://a.example.com")
	if err == nil || errors.Is(err, ErrLoginExpired) {
		t.Errorf("Load(corrupt file) = %v, want a parse error (not a silent re-login)", err)
	}
	if err := s.Save("https://a.example.com", &Credential{IDToken: "x"}); err == nil {
		t.Error("Save over a corrupt file must fail rather than discard the other remotes")
	}
}
