// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestStoreRoundTrip(t *testing.T) {
	store := NewStoreAt(filepath.Join(t.TempDir(), "credentials.json"))

	if _, err := store.Load("https://patchy.example"); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("Load(empty store) = %v, want ErrLoginExpired", err)
	}
	cred := &Credential{
		IDToken:       "raw.jwt.value",
		IDTokenExpiry: time.Now().Add(time.Hour).Truncate(time.Second),
		Token:         &oauth2.Token{AccessToken: "at", RefreshToken: "rt"},
	}
	if err := store.Save("https://Patchy.Example/", cred); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Normalization: different spellings of the same remote hit one entry.
	got, err := store.Load("https://patchy.example")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.IDToken != cred.IDToken || got.Token.RefreshToken != "rt" {
		t.Errorf("Load = %+v, want the saved credential", got)
	}
	if err := store.Delete("https://patchy.example/"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Load("https://patchy.example"); !errors.Is(err, ErrLoginExpired) {
		t.Errorf("Load after Delete = %v, want ErrLoginExpired", err)
	}
}

// fakeIssuer is a minimal OIDC provider: discovery, JWKS, and a token
// endpoint that checks the PKCE verifier and mints signed ID tokens.
type fakeIssuer struct {
	t        *testing.T
	key      *rsa.PrivateKey
	srv      *httptest.Server
	clientID string

	sawAuthorize url.Values
	refreshed    bool
}

func newFakeIssuer(t *testing.T, clientID string) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	fi := &fakeIssuer{t: t, key: key, clientID: clientID}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                fi.srv.URL,
			"authorization_endpoint":                fi.srv.URL + "/auth",
			"token_endpoint":                        fi.srv.URL + "/token",
			"jwks_uri":                              fi.srv.URL + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, _ *http.Request) {
		pub := fi.key.Public().(*rsa.PublicKey)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "test",
				"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		})
	})
	// The "browser": redirect straight back to the client with a code.
	mux.HandleFunc("GET /auth", func(w http.ResponseWriter, r *http.Request) {
		fi.sawAuthorize = r.URL.Query()
		redirect := r.URL.Query().Get("redirect_uri")
		state := r.URL.Query().Get("state")
		http.Redirect(w, r, redirect+"?code=test-code&state="+url.QueryEscape(state), http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		switch form.Get("grant_type") {
		case "authorization_code":
			if form.Get("code_verifier") == "" {
				http.Error(w, "missing code_verifier", http.StatusBadRequest)
				return
			}
		case "refresh_token":
			if form.Get("refresh_token") != "rt-1" {
				http.Error(w, "bad refresh token", http.StatusBadRequest)
				return
			}
			fi.refreshed = true
		}
		refresh := "rt-1"
		if fi.refreshed {
			refresh = "rt-2" // rotation, dex-style
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "token_type": "Bearer", "expires_in": 3600,
			"refresh_token": refresh,
			"id_token":      fi.mintIDToken(time.Hour),
		})
	})
	fi.srv = httptest.NewServer(mux)
	t.Cleanup(fi.srv.Close)
	return fi
}

// mintIDToken signs a minimal RS256 ID token.
func (fi *fakeIssuer) mintIDToken(ttl time.Duration) string {
	fi.t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`))
	claims, _ := json.Marshal(map[string]any{
		"iss": fi.srv.URL, "aud": fi.clientID, "sub": "dev",
		"email": "dev@example.com",
		"exp":   time.Now().Add(ttl).Unix(), "iat": time.Now().Unix(),
	})
	payload := base64.RawURLEncoding.EncodeToString(claims)
	signed := header + "." + payload
	digest := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, fi.key, crypto.SHA256, digest[:])
	if err != nil {
		fi.t.Fatalf("sign id token: %v", err)
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// newFakeRemote serves /api/v1/auth/info pointing at the issuer.
func newFakeRemote(t *testing.T, issuer, clientID string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/auth/info", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(AuthInfo{
			Mode: "oidc", Issuer: issuer, ClientID: clientID, Scopes: []string{"profile", "email"},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// completeLogin drives the printed auth URL like a browser would, following
// the issuer redirect back to the loopback callback.
func completeLogin(t *testing.T, out *syncBuilder, done chan error) {
	t.Helper()
	urlRe := regexp.MustCompile(`https?://\S+/auth\?\S+`)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if m := urlRe.FindString(out.String()); m != "" {
			resp, err := http.Get(m) // follows the redirect chain to /callback
			if err != nil {
				t.Errorf("drive auth url: %v", err)
			} else {
				_ = resp.Body.Close()
			}
			return
		}
		select {
		case err := <-done:
			t.Fatalf("login finished before printing the URL: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("auth URL never printed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLoginFlowAndRefreshRotation(t *testing.T) {
	issuer := newFakeIssuer(t, "evolve")
	remoteSrv := newFakeRemote(t, issuer.srv.URL, "evolve")
	store := NewStoreAt(filepath.Join(t.TempDir(), "credentials.json"))

	var out syncBuilder
	done := make(chan error, 1)
	go func() { done <- Login(context.Background(), store, remoteSrv.URL, true, &out) }()
	completeLogin(t, &out, done)
	if err := <-done; err != nil {
		t.Fatalf("Login: %v", err)
	}

	// PKCE actually happened: S256 challenge on authorize, verifier on token.
	if got := issuer.sawAuthorize.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", got)
	}
	if issuer.sawAuthorize.Get("code_challenge") == "" {
		t.Error("authorize request carried no code_challenge")
	}
	// Scopes are the advertised set ∪ {openid, offline_access}.
	scope := issuer.sawAuthorize.Get("scope")
	for _, want := range []string{"openid", "offline_access", "profile", "email"} {
		if !strings.Contains(scope, want) {
			t.Errorf("scope %q lacks %q", scope, want)
		}
	}

	cred, err := store.Load(remoteSrv.URL)
	if err != nil {
		t.Fatalf("Load after login: %v", err)
	}
	if cred.Token.RefreshToken != "rt-1" {
		t.Errorf("refresh token = %q, want rt-1", cred.Token.RefreshToken)
	}

	// Bearer with a live ID token: no refresh.
	info := &AuthInfo{Mode: "oidc", Issuer: issuer.srv.URL, ClientID: "evolve"}
	bearer, err := Bearer(context.Background(), store, remoteSrv.URL, info)
	if err != nil {
		t.Fatalf("Bearer: %v", err)
	}
	if bearer != cred.IDToken {
		t.Error("live bearer should be the stored ID token")
	}

	// Expire the stored ID token: Bearer must refresh and save through the
	// rotated refresh token.
	cred.IDTokenExpiry = time.Now().Add(-time.Minute)
	if err := store.Save(remoteSrv.URL, cred); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := Bearer(context.Background(), store, remoteSrv.URL, info); err != nil {
		t.Fatalf("Bearer(refresh): %v", err)
	}
	if !issuer.refreshed {
		t.Error("refresh grant never reached the issuer")
	}
	rotated, err := store.Load(remoteSrv.URL)
	if err != nil {
		t.Fatalf("Load after refresh: %v", err)
	}
	if rotated.Token.RefreshToken != "rt-2" {
		t.Errorf("refresh token after rotation = %q, want rt-2", rotated.Token.RefreshToken)
	}
}

func TestBearerModeNoneIsEmpty(t *testing.T) {
	store := NewStoreAt(filepath.Join(t.TempDir(), "credentials.json"))
	bearer, err := Bearer(context.Background(), store, "http://dev.local", &AuthInfo{Mode: "none"})
	if err != nil || bearer != "" {
		t.Errorf("Bearer(mode none) = (%q, %v), want empty and nil", bearer, err)
	}
}

// syncBuilder is a mutex-guarded strings.Builder (the login goroutine writes
// while the test polls).
type syncBuilder struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuilder) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuilder) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
