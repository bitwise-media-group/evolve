// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
)

// ErrUINotBundled is returned by [Server.Snapshot] when the SPA assets were not
// compiled in (a bare `go build`); snapshot export needs the single-file shell.
var ErrUINotBundled = errors.New("web UI not bundled in this build (build with `make build`)")

// snapshotMarker is where the dataset blob is injected. The browser reads
// window.__EVOLVE_SNAPSHOT__ on boot and, when present, runs offline from it.
const snapshotGlobal = "window.__EVOLVE_SNAPSHOT__"

// Snapshot renders a self-contained HTML page: the embedded single-file SPA with
// the current dataset inlined, so it opens offline over file://. The exported
// page carries no preselected view (view = null); the in-browser "Save snapshot"
// builds its own with the active filters. Returns [ErrUINotBundled] when no SPA
// is embedded.
func (s *Server) Snapshot() ([]byte, error) {
	ds, err := BuildDataset(s.repo, s.toolVersion)
	if err != nil {
		return nil, err
	}
	index, ok := indexHTML()
	if !ok {
		return nil, ErrUINotBundled
	}
	return injectSnapshot(index, ds)
}

// indexHTML reads the embedded SPA shell.
func indexHTML() ([]byte, bool) {
	assets, ok := uiAssets()
	if !ok {
		return nil, false
	}
	b, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		return nil, false
	}
	return b, true
}

// injectSnapshot inserts a script defining the snapshot global at the top of
// <head>, so it runs before the (deferred) module bundle. json.Marshal escapes
// <, >, and & to \u00xx, so payload data containing "</script>" cannot break out
// of the script element.
func injectSnapshot(index []byte, ds *Dataset) ([]byte, error) {
	blob, err := json.Marshal(struct {
		Dataset *Dataset        `json:"dataset"`
		View    json.RawMessage `json:"view"`
	}{Dataset: ds, View: nil})
	if err != nil {
		return nil, err
	}
	script := []byte("<script>" + snapshotGlobal + " = " + string(blob) + ";</script>")

	if i := bytes.Index(index, []byte("<head>")); i >= 0 {
		at := i + len("<head>")
		return concat(index[:at], script, index[at:]), nil
	}
	if i := bytes.Index(index, []byte("</head>")); i >= 0 {
		return concat(index[:i], script, index[i:]), nil
	}
	return nil, errors.New("snapshot: no <head> in the SPA shell to inject into")
}

func concat(parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
