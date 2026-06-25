// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package web

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/layout"
)

// Server is the read-only viewer backend: it rebuilds the [Dataset] from the
// committed results on each request and streams a change notification when the
// results files are rewritten on disk (see [Server.Watch]). It owns no run state
// and never mutates the repository.
type Server struct {
	repo        *layout.Repo
	toolVersion string
	log         *slog.Logger
	broker      *broker
}

// NewServer builds a viewer server over the detected repository. toolVersion is
// stamped into the dataset; log may be nil (a discarding logger is used).
func NewServer(repo *layout.Repo, toolVersion string, log *slog.Logger) *Server {
	if log == nil {
		log = slog.New(slog.NewTextHandler(discard{}, nil))
	}
	return &Server{repo: repo, toolVersion: toolVersion, log: log, broker: newBroker()}
}

// Handler builds the HTTP routes: the read-only API, the SSE stream, and the
// embedded single-page app (with SPA fallback) on everything else.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/results", s.handleResults)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.Handle("/", s.staticHandler())
	return mux
}

// handleResults serves the dataset, freshly assembled so an open browser that
// refetches after a change sees the latest committed results.
func (s *Server) handleResults(w http.ResponseWriter, r *http.Request) {
	ds, err := BuildDataset(s.repo, s.toolVersion)
	if err != nil {
		s.log.ErrorContext(r.Context(), "viewer: build dataset", slog.Any("error", err))
		http.Error(w, "failed to load results", http.StatusInternalServerError)
		return
	}
	writeJSON(w, ds)
}

// staticHandler serves the embedded SPA. Unknown paths fall back to index.html
// so client-side routing works; when no bundle is embedded (a bare `go build`),
// it serves a short message pointing at `make build`.
func (s *Server) staticHandler() http.Handler {
	assets, ok := uiAssets()
	if !ok {
		return http.HandlerFunc(stubPage)
	}
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			clean = "index.html"
		}
		if _, err := fs.Stat(assets, clean); err != nil {
			// Not a real asset: serve the SPA shell so the client router handles it.
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}

// stubPage is served when the SPA bundle was not compiled in.
func stubPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8">` +
		`<title>evolve view</title>` +
		`<body style="font-family:system-ui;max-width:40rem;margin:4rem auto;padding:0 1rem">` +
		`<h1>Web UI not bundled</h1>` +
		`<p>This build of <code>evolve</code> was compiled without the report-viewer assets. ` +
		`Build with <code>make build</code> (which builds the UI and compiles with ` +
		`<code>-tags withui</code>), or use a release binary.</p>`))
}

// writeJSON encodes v as the response body. HTML escaping stays on (the default)
// so the payload is safe to inline in a <script> for snapshot export.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// discard is an io.Writer sink for the fallback logger.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
