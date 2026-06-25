// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package web

import (
	"errors"
	"strings"
	"testing"
)

func TestInjectSnapshot(t *testing.T) {
	index := []byte("<!doctype html><html><head><title>x</title></head><body></body></html>")
	ds := &Dataset{Repo: "demo", Rows: []Row{{ID: "r1", Type: typeEval, Status: statusPass}}}

	out, err := injectSnapshot(index, ds)
	if err != nil {
		t.Fatalf("injectSnapshot: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, snapshotGlobal+" = ") {
		t.Errorf("missing snapshot global assignment:\n%s", got)
	}
	if !strings.Contains(got, `"id":"r1"`) {
		t.Errorf("dataset not inlined:\n%s", got)
	}
	// The injected script must sit inside <head>, before the title.
	head := strings.Index(got, "<head>")
	script := strings.Index(got, snapshotGlobal)
	title := strings.Index(got, "<title>")
	if head >= script || script >= title {
		t.Errorf("script not injected at top of head (head=%d script=%d title=%d)", head, script, title)
	}
}

// A "</script>" inside the data must be escaped so it cannot terminate the
// injected <script> element.
func TestInjectSnapshotEscapesData(t *testing.T) {
	index := []byte("<head></head>")
	ds := &Dataset{Rows: []Row{{ID: "</script><script>alert(1)</script>"}}}

	out, err := injectSnapshot(index, ds)
	if err != nil {
		t.Fatalf("injectSnapshot: %v", err)
	}
	got := string(out)
	// The shell has no scripts of its own, so the only legitimate tags are the
	// single injected pair. An unescaped payload "</script>" would add more.
	if c := strings.Count(got, "<script>"); c != 1 {
		t.Errorf("<script> count = %d, want 1 (payload broke out of the element)", c)
	}
	if c := strings.Count(got, "</script>"); c != 1 {
		t.Errorf("</script> count = %d, want 1 (payload broke out of the element)", c)
	}
}

func TestInjectSnapshotFallbackAndError(t *testing.T) {
	// No <head> open tag, but a closing one: inject before </head>.
	out, err := injectSnapshot([]byte("<html></head><body></body>"), &Dataset{})
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if i, j := strings.Index(string(out), snapshotGlobal), strings.Index(string(out), "</head>"); i < 0 || i >= j {
		t.Errorf("script not injected before </head> (script=%d </head>=%d)", i, j)
	}

	// No head at all: an error, not a silent bad page.
	if _, err := injectSnapshot([]byte("<html><body></body></html>"), &Dataset{}); err == nil {
		t.Error("expected an error when the shell has no head")
	}
}

func TestServerSnapshotWithoutBundle(t *testing.T) {
	if _, ok := uiAssets(); ok {
		t.Skip("built with -tags withui; snapshot has a bundle to inject into")
	}
	repo, _ := fixtureRepo(t)
	_, err := NewServer(repo, "v1", nil).Snapshot()
	if !errors.Is(err, ErrUINotBundled) {
		t.Errorf("err = %v, want ErrUINotBundled", err)
	}
}
