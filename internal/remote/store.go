// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// ErrLoginExpired means no usable credential exists for the remote — never
// stored, or its refresh token no longer works. The fix is `evolve login`.
var ErrLoginExpired = errors.New("not logged in (or the session expired): run `evolve login`")

// Credential is one remote's stored login: the verified ID token presented
// as the bearer, and the OAuth2 token carrying the refresh token that renews
// it.
type Credential struct {
	// IDToken is the raw ID token (the bearer the API verifies).
	IDToken string `json:"idToken"`
	// IDTokenExpiry is the verified token's expiry.
	IDTokenExpiry time.Time `json:"idTokenExpiry"`
	// Token is the OAuth2 access/refresh token pair.
	Token *oauth2.Token `json:"token"`
}

// Store persists credentials per normalized remote URL in one user-level
// file. UserConfigDir, not UserCacheDir: these are durable secrets, not a
// purgeable cache — evolve's first user-level file (everything else is
// repo-scoped configuration).
type Store struct {
	path string
}

// NewStore opens the default user-level store
// (<UserConfigDir>/evolve/credentials.json).
func NewStore() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("remote: user config dir: %w", err)
	}
	return &Store{path: filepath.Join(dir, "evolve", "credentials.json")}, nil
}

// NewStoreAt opens a store at an explicit path (tests).
func NewStoreAt(path string) *Store { return &Store{path: path} }

// NormalizeRemote canonicalizes a remote URL as the credential key:
// lowercase, no trailing slash.
func NormalizeRemote(u string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(u)), "/")
}

// load reads the whole file; a missing file is an empty store.
func (s *Store) load() (map[string]*Credential, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]*Credential{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("remote: read credentials: %w", err)
	}
	creds := map[string]*Credential{}
	if err := json.Unmarshal(raw, &creds); err != nil {
		return nil, fmt.Errorf("remote: parse credentials %s: %w", s.path, err)
	}
	return creds, nil
}

// save writes the whole file atomically, 0700 dir / 0600 file.
func (s *Store) save(creds map[string]*Credential) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("remote: credentials dir: %w", err)
	}
	raw, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("remote: marshal credentials: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("remote: write credentials: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("remote: place credentials: %w", err)
	}
	return nil
}

// Load returns the remote's credential, or ErrLoginExpired when absent.
func (s *Store) Load(remote string) (*Credential, error) {
	creds, err := s.load()
	if err != nil {
		return nil, err
	}
	cred, ok := creds[NormalizeRemote(remote)]
	if !ok || cred.IDToken == "" {
		return nil, ErrLoginExpired
	}
	return cred, nil
}

// Save stores the remote's credential (save-through on refresh rotation).
func (s *Store) Save(remote string, cred *Credential) error {
	creds, err := s.load()
	if err != nil {
		return err
	}
	creds[NormalizeRemote(remote)] = cred
	return s.save(creds)
}

// Delete forgets the remote's credential; deleting an absent one is fine.
func (s *Store) Delete(remote string) error {
	creds, err := s.load()
	if err != nil {
		return err
	}
	delete(creds, NormalizeRemote(remote))
	return s.save(creds)
}
