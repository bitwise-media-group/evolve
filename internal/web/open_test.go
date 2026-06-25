// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package web

import (
	"slices"
	"testing"
)

func TestBrowserOpenCommand(t *testing.T) {
	name, args := browserOpenCommand("http://127.0.0.1:8080")
	if name == "" {
		t.Fatal("no browser-open command for this platform")
	}
	if !slices.Contains(args, "http://127.0.0.1:8080") {
		t.Errorf("args %v do not include the url", args)
	}
}
