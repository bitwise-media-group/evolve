// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/evalspec"
)

func TestTriggerContentHashTracksFrontmatter(t *testing.T) {
	base := triggerContentHash([]byte("---\nname: s\ndescription: original\n---\nbody\n"))

	// Body-only edits leave the frontmatter hash unchanged.
	sameFM := triggerContentHash([]byte("---\nname: s\ndescription: original\n---\ndifferent body\n"))
	if base != sameFM {
		t.Error("body edit must not change the trigger content hash")
	}

	// A frontmatter edit changes it.
	if base == triggerContentHash([]byte("---\nname: s\ndescription: changed\n---\nbody\n")) {
		t.Error("frontmatter edit must change the trigger content hash")
	}

	// A SKILL.md without frontmatter still hashes to a stable, distinct value.
	none := triggerContentHash([]byte("# no frontmatter\n"))
	if none == "" || none == base {
		t.Errorf("no-frontmatter hash = %q (base %q)", none, base)
	}
	if none != triggerContentHash([]byte("# different body, still no frontmatter\n")) {
		t.Error("no-frontmatter hash must be stable regardless of body")
	}
}

func TestSkillContentHash(t *testing.T) {
	write := func(dir, rel, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	dir := t.TempDir()
	write(dir, "SKILL.md", "---\nname: s\n---\nbody")
	write(dir, "references/guide.md", "reference content")
	base, err := skillContentHash(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Stable across recomputation, and unaffected by ignored noise.
	write(dir, ".DS_Store", "junk")
	write(dir, ".git/HEAD", "ref: refs/heads/main")
	again, err := skillContentHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if base != again {
		t.Error("dotfiles/dot-dirs must not affect the skill content hash")
	}

	// A reference-file edit changes it.
	write(dir, "references/guide.md", "edited reference content")
	if edited, _ := skillContentHash(dir); edited == base {
		t.Error("editing a reference file must change the skill content hash")
	}

	// Differing file content changes the hash.
	a := t.TempDir()
	write(a, "SKILL.md", "x")
	write(a, "b", "y")
	b := t.TempDir()
	write(b, "SKILL.md", "x")
	write(b, "b", "z")
	ha, _ := skillContentHash(a)
	hb, _ := skillContentHash(b)
	if ha == hb {
		t.Error("differing file content must change the hash")
	}
}

func TestSpecHash(t *testing.T) {
	a := evalspec.Trigger{Query: "do x", ShouldTrigger: true}
	if specHash(a) != specHash(evalspec.Trigger{Query: "do x", ShouldTrigger: true}) {
		t.Error("identical triggers must hash equal")
	}
	if specHash(a) == specHash(evalspec.Trigger{Query: "do x", ShouldTrigger: false}) {
		t.Error("a should_trigger flip must change the spec hash")
	}
	if specHash(a) == specHash(evalspec.Trigger{Query: "do y", ShouldTrigger: true}) {
		t.Error("a query edit must change the spec hash")
	}

	e := evalspec.Eval{ID: "1", Prompt: "p", Expectations: []string{"writes a file"}}
	if specHash(e) != specHash(evalspec.Eval{ID: "1", Prompt: "p", Expectations: []string{"writes a file"}}) {
		t.Error("identical evals must hash equal")
	}
	if specHash(e) == specHash(evalspec.Eval{ID: "1", Prompt: "changed", Expectations: []string{"writes a file"}}) {
		t.Error("a prompt edit must change the spec hash")
	}
}
