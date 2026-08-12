// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/layout"
)

// testEvalSet builds a plugin-relative EvalSet over a temp tree.
func testEvalSet(root, skill string) layout.EvalSet {
	p := layout.Plugin{
		Name:      "test-plugin",
		Dir:       root,
		SkillsDir: filepath.Join(root, "skills"),
		EvalsDir:  filepath.Join(root, "evals"),
	}
	return layout.EvalSet{
		Plugin:     p,
		Skill:      skill,
		SkillDir:   filepath.Join(p.SkillsDir, skill),
		ResultsDir: filepath.Join(p.EvalsDir, skill),
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestBundleDeterminism(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "skills/skill-a/SKILL.md"), "# a\n")
	writeFile(t, filepath.Join(dir, "skills/skill-b/SKILL.md"), "# b\n")
	writeFile(t, filepath.Join(dir, "skills/skill-b/scripts/run.sh"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(dir, "evals/skill-a/triggers.json"), `{"queries":[]}`)
	writeFile(t, filepath.Join(dir, "evals/skill-a/evals.json"), `{"evals":[]}`)
	writeFile(t, filepath.Join(dir, "evals/skill-a/fixture.txt"), "fixture\n")
	writeFile(t, filepath.Join(dir, "evals/skill-a/results.json"), `{"schema":5}`)

	set := testEvalSet(dir, "skill-a")

	a, err := EvalsBundle(set)
	if err != nil {
		t.Fatalf("EvalsBundle: %v", err)
	}
	b, err := EvalsBundle(set)
	if err != nil {
		t.Fatalf("EvalsBundle(again): %v", err)
	}
	if a.Digest != b.Digest {
		t.Errorf("bundle digests differ across identical builds: %s vs %s", a.Digest, b.Digest)
	}

	entries, err := ReadBundle(a)
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}
	if _, ok := entries["evals/skill-a/results.json"]; ok {
		t.Error("results.json leaked into the bundle (digests would churn every run)")
	}
	for _, want := range []string{
		"skills/skill-a/SKILL.md",
		"evals/skill-a/triggers.json",
		"evals/skill-a/evals.json",
		"evals/skill-a/fixture.txt",
	} {
		if _, ok := entries[want]; !ok {
			t.Errorf("bundle lacks %s (has %v)", want, keys(entries))
		}
	}
	if _, ok := entries["skills/skill-b/SKILL.md"]; ok {
		t.Error("evals bundle carried a sibling skill")
	}

	trig, err := TriggersBundle(set.Plugin)
	if err != nil {
		t.Fatalf("TriggersBundle: %v", err)
	}
	tentries, err := ReadBundle(trig)
	if err != nil {
		t.Fatalf("ReadBundle(triggers): %v", err)
	}
	for _, want := range []string{
		"skills/skill-a/SKILL.md",
		"skills/skill-b/SKILL.md",
		"skills/skill-b/scripts/run.sh",
		"evals/skill-a/triggers.json",
	} {
		if _, ok := tentries[want]; !ok {
			t.Errorf("triggers bundle lacks %s (has %v)", want, keys(tentries))
		}
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// fakePatchy is an in-memory remote-evaluation service (auth mode none).
type fakePatchy struct {
	t          *testing.T
	srv        *httptest.Server
	blobs      map[string][]byte
	submission *Submission
	cancelled  atomic.Bool
	// puts counts workspace uploads, so tests can assert dedupe.
	puts atomic.Int32
	// connections counts SSE attempts; the first drops mid-stream to
	// exercise reconnect.
	connections atomic.Int32
	settled     UnitStatusWire
}

func newFakePatchy(t *testing.T) *fakePatchy {
	t.Helper()
	fp := &fakePatchy{t: t, blobs: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/auth/info", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(AuthInfo{Mode: "none"})
	})
	mux.HandleFunc("HEAD /api/v1/workspaces/{digest}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := fp.blobs[r.PathValue("digest")]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /api/v1/workspaces/{digest}", func(w http.ResponseWriter, r *http.Request) {
		fp.puts.Add(1)
		raw, _ := io.ReadAll(r.Body)
		fp.blobs[r.PathValue("digest")] = raw
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("POST /api/v1/evaluations", func(w http.ResponseWriter, r *http.Request) {
		var sub Submission
		if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fp.submission = &sub
		units := make([]string, len(sub.Units))
		for i := range units {
			units[i] = fmt.Sprintf("eval-1-u%03d", i)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(SubmissionResponse{Name: "eval-1", Units: units})
	})
	mux.HandleFunc("GET /api/v1/evaluations/{name}", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(EvaluationStatusWire{
			Name: "eval-1", Phase: "Running", UnitsTotal: 1,
			Units: []UnitStatusWire{{Name: "eval-1-u000", Phase: "Running"}},
		})
	})
	mux.HandleFunc("GET /api/v1/evaluations/{name}/events", func(w http.ResponseWriter, r *http.Request) {
		n := fp.connections.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeEvent := func(event string, v any) {
			raw, _ := json.Marshal(v)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
			flusher.Flush()
		}
		writeEvent(SSEEventUnit, UnitStatusWire{Name: "eval-1-u000", Phase: "Running"})
		if n == 1 {
			return // drop mid-stream: the client must reconnect
		}
		writeEvent(SSEEventUnit, fp.settled)
		writeEvent(SSEEventEnd, EvaluationStatusWire{
			Name: "eval-1", Phase: "Complete", UnitsTotal: 1, UnitsComplete: 1,
			Units: []UnitStatusWire{fp.settled},
		})
	})
	mux.HandleFunc("DELETE /api/v1/evaluations/{name}", func(w http.ResponseWriter, _ *http.Request) {
		fp.cancelled.Store(true)
		w.WriteHeader(http.StatusAccepted)
	})
	fp.srv = httptest.NewServer(mux)
	t.Cleanup(fp.srv.Close)
	return fp
}

func newTestClient(t *testing.T, fp *fakePatchy) *Client {
	t.Helper()
	store := NewStoreAt(filepath.Join(t.TempDir(), "credentials.json"))
	client, err := NewClient(context.Background(), store, fp.srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestClientWorkspaceDedupe(t *testing.T) {
	fp := newFakePatchy(t)
	c := newTestClient(t, fp)
	ctx := context.Background()

	digest := "aa11bb22cc33dd44ee55ff6600112233445566778899aabbccddeeff00112233"
	cached, err := c.HasWorkspace(ctx, digest)
	if err != nil || cached {
		t.Fatalf("HasWorkspace(absent) = (%v, %v), want (false, nil)", cached, err)
	}
	if err := c.UploadWorkspace(ctx, digest, []byte("bundle")); err != nil {
		t.Fatalf("UploadWorkspace: %v", err)
	}
	cached, err = c.HasWorkspace(ctx, digest)
	if err != nil || !cached {
		t.Errorf("HasWorkspace(cached) = (%v, %v), want (true, nil)", cached, err)
	}
}

func TestClientEventsReconnects(t *testing.T) {
	fp := newFakePatchy(t)
	fp.settled = UnitStatusWire{
		Name: "eval-1-u000", Phase: "Complete",
		Result: &UnitResult{Tier: 2, Model: "anthropic/claude-sonnet-5", Harness: "claude",
			Summary: ResultSummary{CasesPassed: 1, Outcome: "ok"},
			Entry:   json.RawMessage(`{"schema":5}`)},
	}
	c := newTestClient(t, fp)

	var units []UnitStatusWire
	final, err := c.Events(context.Background(), "eval-1",
		func(u UnitStatusWire) { units = append(units, u) }, nil)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if fp.connections.Load() < 2 {
		t.Errorf("connections = %d, want a reconnect after the drop", fp.connections.Load())
	}
	if final == nil || final.Phase != "Complete" {
		t.Fatalf("final = %+v, want Complete", final)
	}
	var settled bool
	for _, u := range units {
		if u.Name == "eval-1-u000" && u.Result != nil {
			settled = true
		}
	}
	if !settled {
		t.Error("settled unit with a result never reached onUnit")
	}
}

func TestClientCancel(t *testing.T) {
	fp := newFakePatchy(t)
	c := newTestClient(t, fp)
	if err := c.Cancel(context.Background(), "eval-1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !fp.cancelled.Load() {
		t.Error("cancel never reached the server")
	}
}
