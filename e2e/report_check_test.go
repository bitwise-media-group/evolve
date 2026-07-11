// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReportCheckGate exercises `report --check`'s user-facing gate output and
// exit codes against a real evolve binary — the integration coverage the cobra
// RunE glue in cmd/evolve/report.go is the right layer for. It needs no live
// model or credentials: the gate reads committed evidence (here, deliberately
// absent) and classifies plugins by manifest maturity, so it is fully hermetic
// and deterministic.
//
// The fixture is a single unstable-maturity plugin (0.5.0) that authored
// triggers but has no stored results. Under --strict:
//   - when its maturity IS gated, the missing evidence FAILs and evolve exits 1;
//   - when its maturity is NOT gated, the same absence only WARNs and exits 0.
//
// It also pins two behaviors surfaced in review: every breach names the plugin
// ("solo:"), and a triggers-only plugin never emits a spurious evals breach.
func TestReportCheckGate(t *testing.T) {
	bin := buildEvolve(t)

	repo := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A 0.5.0 plugin classifies as unstable maturity. Triggers authored, no evals
	// and no results.* file — so the gate sees missing evidence for a real plugin.
	write(".claude-plugin/plugin.json", `{"name":"solo","version":"0.5.0"}`)
	write("skills/solo-skill/SKILL.md", "---\nname: solo-skill\n---\nbody\n")
	write("evals/solo-skill/triggers.json", `{"triggers":[{"query":"q","should_trigger":true}]}`)

	run := func(t *testing.T, maturity string) (stderr string, exit int) {
		t.Helper()
		cmd := exec.Command(bin, "report", "--check", "--strict", "--maturity", maturity, "--root", repo)
		var out, errBuf bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		err := cmd.Run()
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("run evolve report: %v\nstderr:\n%s", err, errBuf.String())
			}
			exit = exitErr.ExitCode()
		}
		return errBuf.String(), exit
	}

	t.Run("gated plugin with missing evidence fails", func(t *testing.T) {
		stderr, exit := run(t, "unstable") // the plugin is 0.5.0 -> unstable, so gated
		if exit != 1 {
			t.Errorf("exit = %d, want 1 for a gated plugin with missing evidence", exit)
		}
		if !strings.Contains(stderr, "FAIL: solo: triggers: no stored results") {
			t.Errorf("stderr missing a plugin-named FAIL line:\n%s", stderr)
		}
		// A triggers-only plugin must not be gated on a tier it never authored.
		if strings.Contains(stderr, "evals:") {
			t.Errorf("triggers-only plugin emitted an evals breach:\n%s", stderr)
		}
	})

	t.Run("ungated plugin with missing evidence only warns", func(t *testing.T) {
		stderr, exit := run(t, "stable") // 0.5.0 is unstable, so NOT gated here
		if exit != 0 {
			t.Errorf("exit = %d, want 0 when the plugin's maturity is not gated", exit)
		}
		if !strings.Contains(stderr, "WARN: solo: triggers: no stored results") {
			t.Errorf("stderr missing a plugin-named WARN line:\n%s", stderr)
		}
		if strings.Contains(stderr, "FAIL:") {
			t.Errorf("ungated plugin must not FAIL:\n%s", stderr)
		}
	})
}
