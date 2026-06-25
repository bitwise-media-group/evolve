// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleResults(t *testing.T) {
	repo, _ := fixtureRepo(t)
	srv := httptest.NewServer(NewServer(repo, "v1", nil).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/results")
	if err != nil {
		t.Fatalf("GET /api/results: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var ds Dataset
	if err := json.NewDecoder(resp.Body).Decode(&ds); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(ds.Rows) != 4 {
		t.Errorf("got %d rows, want 4", len(ds.Rows))
	}
}

// TestStaticHandlerStub asserts the no-bundle build serves the guidance page.
// Tests compile without -tags withui, so uiAssets reports no bundle.
func TestStaticHandlerStub(t *testing.T) {
	if _, ok := uiAssets(); ok {
		t.Skip("built with -tags withui; the stub path is not exercised")
	}
	repo, _ := fixtureRepo(t)
	srv := httptest.NewServer(NewServer(repo, "v1", nil).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
	body := readAll(t, resp)
	if !strings.Contains(body, "Web UI not bundled") {
		t.Errorf("stub body missing guidance, got: %q", body)
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}
