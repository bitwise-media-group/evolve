// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestScanLineGoogle(t *testing.T) {
	g := NewGoogle()
	tests := []struct {
		name     string
		line     string
		skill    string
		wantHit  bool
		wantNote string
	}{
		{
			name:    "activate_skill",
			line:    `{"type":"tool_use","tool_name":"activate_skill","parameters":{"skill":"go-testing"}}`,
			skill:   "go-testing",
			wantHit: true,
		},
		{
			name:    "read_file fallback",
			line:    `{"type":"tool_use","tool_name":"read_file","parameters":{"path":".gemini/skills/go-testing/SKILL.md"}}`,
			skill:   "go-testing",
			wantHit: true,
		},
		{
			name:     "error result",
			line:     `{"type":"result","status":"error","error":{"message":"quota exceeded"}}`,
			skill:    "go-testing",
			wantHit:  false,
			wantNote: "gemini run errored; counted as no-trigger: quota exceeded",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit, note := g.ScanLine([]byte(tt.line), tt.skill)
			if hit != tt.wantHit {
				t.Errorf("hit = %v, want %v", hit, tt.wantHit)
			}
			if note != tt.wantNote {
				t.Errorf("note = %q, want %q", note, tt.wantNote)
			}
		})
	}
}

func TestCountTokensGoogle(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "g-key")
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if got := r.Header.Get("x-goog-api-key"); got != "g-key" {
			t.Errorf("x-goog-api-key = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]int{"totalTokens": 11})
	}))
	defer srv.Close()

	g := NewGoogle()
	g.CountURLBase = srv.URL + "/models/"
	g.Client = srv.Client()
	got, err := g.CountTokens(context.Background(), "gemini-3.5-flash", "hi")
	if err != nil || got != 11 {
		t.Errorf("CountTokens = %d, %v; want 11, nil", got, err)
	}
	if gotPath != "/models/gemini-3.5-flash:countTokens" {
		t.Errorf("path = %q", gotPath)
	}
}
