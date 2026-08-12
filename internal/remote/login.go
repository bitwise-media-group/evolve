// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"slices"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// loginTimeout bounds the whole interactive flow: a browser tab someone
// forgot about must not hold the process forever.
const loginTimeout = 5 * time.Minute

// FetchAuthInfo reads the remote's GET /api/v1/auth/info — everything the
// login flow needs, so the user configures nothing but the URL.
func FetchAuthInfo(ctx context.Context, remoteURL string) (*AuthInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		NormalizeRemote(remoteURL)+"/api/v1/auth/info", nil)
	if err != nil {
		return nil, fmt.Errorf("remote: auth info: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("remote: auth info: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote: auth info: unexpected status %d from %s", resp.StatusCode, remoteURL)
	}
	var info AuthInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return nil, fmt.Errorf("remote: auth info: %w", err)
	}
	return &info, nil
}

// Login runs the OIDC authorization-code + PKCE flow against the remote's
// issuer and persists the credential in the store. The client is public (no
// secret); the redirect is an ephemeral localhost listener. noBrowser skips
// the best-effort browser open — the URL is always printed either way.
func Login(ctx context.Context, store *Store, remoteURL string, noBrowser bool, out io.Writer) error {
	info, err := FetchAuthInfo(ctx, remoteURL)
	if err != nil {
		return err
	}
	if info.Mode == "none" {
		fmt.Fprintf(out, "%s runs with authentication disabled; no login needed\n", remoteURL)
		return nil
	}

	provider, err := gooidc.NewProvider(ctx, info.Issuer)
	if err != nil {
		return fmt.Errorf("remote: discover issuer %s: %w", info.Issuer, err)
	}

	// The ephemeral-port callback listener; the OIDC client must allow
	// http://127.0.0.1:*/callback redirects.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("remote: callback listener: %w", err)
	}
	defer func() { _ = ln.Close() }()

	scopes := info.Scopes
	for _, s := range []string{gooidc.ScopeOpenID, "offline_access"} {
		if !slices.Contains(scopes, s) {
			scopes = append(scopes, s)
		}
	}
	cfg := oauth2.Config{
		ClientID:    info.ClientID,
		Endpoint:    provider.Endpoint(),
		RedirectURL: fmt.Sprintf("http://%s/callback", ln.Addr()),
		Scopes:      scopes,
	}
	verifier := oauth2.GenerateVerifier()
	state, err := randomToken()
	if err != nil {
		return err
	}
	authURL := cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	fmt.Fprintf(out, "Open this URL to sign in:\n\n  %s\n\n", authURL)
	if !noBrowser {
		openBrowser(authURL) // best-effort; the printed URL is the fallback
	}

	code, err := waitForCallback(ctx, ln, state)
	if err != nil {
		return err
	}
	token, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return fmt.Errorf("remote: code exchange: %w", err)
	}
	cred, err := credentialFrom(ctx, provider, info.ClientID, token)
	if err != nil {
		return err
	}
	if err := store.Save(remoteURL, cred); err != nil {
		return err
	}
	fmt.Fprintf(out, "Logged in to %s\n", remoteURL)
	return nil
}

// credentialFrom verifies the exchange's ID token and renders the stored
// credential.
func credentialFrom(ctx context.Context, provider *gooidc.Provider, clientID string,
	token *oauth2.Token) (*Credential, error) {
	rawID, _ := token.Extra("id_token").(string)
	if rawID == "" {
		return nil, errors.New("remote: token response carried no id_token")
	}
	idToken, err := provider.Verifier(&gooidc.Config{ClientID: clientID}).Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("remote: verify id token: %w", err)
	}
	return &Credential{IDToken: rawID, IDTokenExpiry: idToken.Expiry, Token: token}, nil
}

// waitForCallback serves the loopback redirect until the code arrives; a
// state mismatch is rejected without ending the wait (stray requests happen).
func waitForCallback(ctx context.Context, ln net.Listener, state string) (string, error) {
	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		if errCode := q.Get("error"); errCode != "" {
			http.Error(w, "sign-in failed: "+errCode, http.StatusBadRequest)
			done <- result{err: fmt.Errorf("remote: authorization failed: %s (%s)",
				errCode, q.Get("error_description"))}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, "<!doctype html><title>evolve</title>"+
			"<body style=\"font-family:sans-serif\"><p>Signed in — return to your terminal.</p></body>")
		done <- result{code: code}
	})
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("remote: waiting for the sign-in callback: %w", ctx.Err())
	case r := <-done:
		return r.code, r.err
	}
}

// Bearer returns a live bearer token for the remote, refreshing (and
// save-through persisting — dex rotates refresh tokens) when the stored ID
// token is stale. A hard refresh failure is ErrLoginExpired.
func Bearer(ctx context.Context, store *Store, remoteURL string, info *AuthInfo) (string, error) {
	if info.Mode == "none" {
		return "", nil
	}
	cred, err := store.Load(remoteURL)
	if err != nil {
		return "", err
	}
	if time.Until(cred.IDTokenExpiry) > 30*time.Second {
		return cred.IDToken, nil
	}
	if cred.Token == nil || cred.Token.RefreshToken == "" {
		return "", ErrLoginExpired
	}

	provider, err := gooidc.NewProvider(ctx, info.Issuer)
	if err != nil {
		return "", fmt.Errorf("remote: discover issuer %s: %w", info.Issuer, err)
	}
	cfg := oauth2.Config{ClientID: info.ClientID, Endpoint: provider.Endpoint()}
	// Force the refresh grant: what is needed is a fresh id_token, and only
	// a refresh mints one — a still-valid access token would be returned
	// as-is.
	stale := *cred.Token
	stale.AccessToken = ""
	stale.Expiry = time.Now().Add(-time.Minute)
	token, err := cfg.TokenSource(ctx, &stale).Token()
	if err != nil {
		return "", fmt.Errorf("%w (refresh failed: %v)", ErrLoginExpired, err)
	}
	fresh, err := credentialFrom(ctx, provider, info.ClientID, token)
	if err != nil {
		return "", fmt.Errorf("%w (refresh failed: %v)", ErrLoginExpired, err)
	}
	if err := store.Save(remoteURL, fresh); err != nil {
		return "", err
	}
	return fresh.IDToken, nil
}

// randomToken mints an unguessable state value.
func randomToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("remote: random state: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// openBrowser launches the platform browser opener, best-effort. This is a
// deliberate setup-time exec exception (like the workspace initializer's
// git), never agent execution.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
