// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package profile

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		values  []string
		want    []Kind
		wantErr bool
	}{
		{"empty", nil, nil, false},
		{"cpu", []string{"cpu"}, []Kind{CPU}, false},
		{"memory", []string{"memory"}, []Kind{Memory}, false},
		// cobra's StringSlice flattens "cpu,memory" and repeated flags into a flat
		// slice before Parse sees it; both forms arrive here identically.
		{"both flattened", []string{"cpu", "memory"}, []Kind{CPU, Memory}, false},
		{"all", []string{"all"}, []Kind{CPU, Memory}, false},
		{"dedup", []string{"cpu", "cpu", "memory"}, []Kind{CPU, Memory}, false},
		{"all plus kind dedups", []string{"all", "memory"}, []Kind{CPU, Memory}, false},
		// Output order is canonical (All order), not input order.
		{"reordered", []string{"memory", "cpu"}, []Kind{CPU, Memory}, false},
		{"case and space insensitive", []string{" CPU ", "Memory"}, []Kind{CPU, Memory}, false},
		{"blank entries ignored", []string{"", "cpu", "  "}, []Kind{CPU}, false},
		{"unknown rejected", []string{"goroutine"}, nil, true},
		{"unknown among valid rejected", []string{"cpu", "bogus"}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(c.values)
			if (err != nil) != c.wantErr {
				t.Fatalf("Parse(%v) error = %v, wantErr %v", c.values, err, c.wantErr)
			}
			if err != nil {
				return
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("Parse(%v) = %v, want %v", c.values, got, c.want)
			}
		})
	}
}

func TestStartWritesProfiles(t *testing.T) {
	dir := t.TempDir()
	stop, paths, err := Start([]Kind{CPU, Memory}, dir)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if stop == nil {
		t.Fatal("Start returned a nil StopFunc for non-empty kinds")
	}
	wantPaths := []string{filepath.Join(dir, "cpu.pprof"), filepath.Join(dir, "mem.pprof")}
	if !slices.Equal(paths, wantPaths) {
		t.Errorf("paths = %v, want %v", paths, wantPaths)
	}

	// Burn a little CPU and allocate so neither profile is trivially empty.
	sink := 0
	for i := range 5_000_000 {
		sink += i % 7
	}
	junk := make([]byte, 1<<20)
	_ = junk
	_ = sink

	if err := stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	for _, p := range wantPaths {
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("stat %s: %v", p, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", p)
		}
	}
}

func TestStartNoKindsIsNoop(t *testing.T) {
	stop, paths, err := Start(nil, t.TempDir())
	if err != nil {
		t.Fatalf("Start(nil): %v", err)
	}
	if stop != nil || paths != nil {
		t.Errorf("Start(nil) = (%v, %v), want (nil, nil)", stop, paths)
	}
}

// TestStartUnwindsOnFailure forces the second profile's file creation to fail (a
// directory where a file should go) and asserts Start unwinds the CPU profile it
// already started — proven by a subsequent CPU profile starting cleanly.
func TestStartUnwindsOnFailure(t *testing.T) {
	dir := t.TempDir()
	// Pre-create mem.pprof as a directory so os.Create fails on the second kind.
	if err := os.Mkdir(filepath.Join(dir, "mem.pprof"), 0o755); err != nil {
		t.Fatal(err)
	}
	stop, _, err := Start([]Kind{CPU, Memory}, dir)
	if err == nil {
		_ = stop()
		t.Fatal("Start: expected error when mem.pprof cannot be created")
	}
	// If the CPU profile leaked, this second start fails ("cpu profiling already
	// in use"); a clean start proves the unwind stopped it.
	stop2, _, err := Start([]Kind{CPU}, t.TempDir())
	if err != nil {
		t.Fatalf("CPU profile leaked from the failed Start: %v", err)
	}
	if err := stop2(); err != nil {
		t.Fatalf("stop2: %v", err)
	}
}
