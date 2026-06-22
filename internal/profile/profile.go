// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
)

// Kind is one profile the --profile flag can request. Adding a profile is a new
// Kind, an entry in All, a fileName case, and a start case — nothing else.
type Kind string

const (
	CPU    Kind = "cpu"
	Memory Kind = "memory"
)

// All is every Kind in canonical order — what --profile=all expands to, and the
// order Parse emits so output is deterministic regardless of how the flag was
// written.
var All = []Kind{CPU, Memory}

// accepted lists the tokens the flag takes, for the parse-error hint.
const accepted = "cpu, memory, all"

// fileName is the pprof file a Kind writes inside the profile directory.
func (k Kind) fileName() string {
	switch k {
	case CPU:
		return "cpu.pprof"
	case Memory:
		return "mem.pprof"
	default:
		return string(k) + ".pprof"
	}
}

// Parse resolves the raw --profile values into a deduped, canonically ordered set
// of Kinds. cobra's StringSlice has already split comma lists and merged repeated
// flags, so values is flat (e.g. ["cpu", "memory"]). "all" expands to every Kind;
// blank entries are ignored; an unknown value is rejected so a typo fails fast
// rather than silently profiling nothing.
func Parse(values []string) ([]Kind, error) {
	want := map[Kind]bool{}
	for _, v := range values {
		switch k := Kind(strings.ToLower(strings.TrimSpace(v))); k {
		case "":
			continue
		case "all":
			for _, a := range All {
				want[a] = true
			}
		case CPU, Memory:
			want[k] = true
		default:
			return nil, fmt.Errorf("unknown profile %q (want %s)", v, accepted)
		}
	}
	var out []Kind
	for _, k := range All {
		if want[k] {
			out = append(out, k)
		}
	}
	return out, nil
}

// StopFunc finalizes profiling: it stops the streaming profiles and writes the
// snapshot profiles, then closes every file, joining any errors. Call it once,
// after the profiled work completes.
type StopFunc func() error

// Start begins the requested profiles, each writing to dir/<kind>.pprof (dir ""
// is the current directory). It returns the StopFunc that finalizes them and the
// paths written, for logging. With no kinds it is a no-op: a nil StopFunc, no
// paths, no error. If a profile fails to start, any already-started ones are
// unwound before the error returns, so a half-started CPU profile never leaks.
func Start(kinds []Kind, dir string) (StopFunc, []string, error) {
	if len(kinds) == 0 {
		return nil, nil, nil
	}
	var (
		stops []func() error
		paths []string
	)
	unwind := func() {
		for _, s := range stops {
			_ = s()
		}
	}
	for _, k := range kinds {
		path := filepath.Join(dir, k.fileName())
		// Create now, before the run, so an unwritable target fails immediately
		// rather than after the work is already done.
		f, err := os.Create(path)
		if err != nil {
			unwind()
			return nil, nil, fmt.Errorf("create %s profile %s: %w", k, path, err)
		}
		stop, err := k.begin(f)
		if err != nil {
			_ = f.Close()
			unwind()
			return nil, nil, err
		}
		stops = append(stops, stop)
		paths = append(paths, path)
	}
	return func() error {
		errs := make([]error, 0, len(stops))
		for _, s := range stops {
			errs = append(errs, s())
		}
		return errors.Join(errs...)
	}, paths, nil
}

// begin starts profiling kind k into f and returns the func that finalizes it and
// closes f. CPU streams until stop; memory is a heap snapshot taken at stop (after
// a GC, so it reflects the live set at the end of the profiled work).
func (k Kind) begin(f *os.File) (func() error, error) {
	switch k {
	case CPU:
		if err := pprof.StartCPUProfile(f); err != nil {
			return nil, fmt.Errorf("start cpu profile: %w", err)
		}
		return func() error {
			pprof.StopCPUProfile()
			return f.Close()
		}, nil
	case Memory:
		return func() error {
			runtime.GC()
			if err := pprof.WriteHeapProfile(f); err != nil {
				_ = f.Close()
				return fmt.Errorf("write memory profile: %w", err)
			}
			return f.Close()
		}, nil
	default:
		_ = f.Close()
		return nil, fmt.Errorf("unsupported profile %q", k)
	}
}
