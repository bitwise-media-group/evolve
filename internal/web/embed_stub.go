// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

//go:build !withui

package web

import "io/fs"

// uiAssets reports that no SPA bundle is embedded. A bare `go build` compiles
// this stub so the package builds without the Vite output; the real assets are
// embedded by embed_ui.go under -tags withui (see `make build`).
func uiAssets() (fs.FS, bool) { return nil, false }
