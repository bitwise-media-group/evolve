// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

//go:build withui

package web

import (
	"embed"
	"io/fs"
)

// uiDist is the built single-file SPA (internal/web/ui/dist), embedded only
// when compiled with -tags withui — `make build` and the release pipeline build
// the Vite app first, then compile with the tag. A bare `go build` uses the stub
// in embed_stub.go instead, so the package always compiles without the bundle.
//
//go:embed all:ui/dist
var uiDist embed.FS

// uiAssets returns the embedded SPA filesystem, rooted at the dist directory.
func uiAssets() (fs.FS, bool) {
	sub, err := fs.Sub(uiDist, "ui/dist")
	if err != nil {
		return nil, false
	}
	return sub, true
}
