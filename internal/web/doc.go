// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

// Package web serves the interactive report viewer: a localhost HTTP server
// hosting an embedded single-page app (built from internal/web/ui) over a
// read-only JSON API.
//
// The data model is a flat list of case rows ([Row]) assembled by [BuildDataset]
// from the committed evals/<skill>/results.<ext> files — one row per trigger
// query or eval case, carrying its plugin, skill, provider, model, type, and
// pass/fail status. The browser does all filtering, sorting, rollup grouping,
// and snapshot export client-side over that list, so the server stays a thin
// read-only seam: GET /api/results returns the dataset, and GET /events is a
// Server-Sent Events stream that emits when the results files change on disk
// (see [Server.Watch]), so any run — CLI, TUI, or CI — that rewrites them
// refreshes an open browser. Launching or controlling runs from the browser is
// deliberately out of scope; the API never mutates.
//
// The SPA assets are embedded behind a build tag (see embed_ui.go): a bare
// `go build` compiles the stub in embed_stub.go and the server reports the UI
// is not bundled, while `make build` (and the release pipeline) build the Vite
// app first and compile with -tags withui to embed the real dist. The snapshot
// export (and `evolve view --out`) injects the dataset into that single-file
// index.html as window.__EVOLVE_SNAPSHOT__, yielding a self-contained page that
// opens offline over file://.
package web
