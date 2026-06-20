// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFrontmatter(t *testing.T) {
	fields, ok, err := Frontmatter(writeSkill(t, "---\nname: go-style\ndescription: \"quoted value\"\nlicense: MIT\n---\nbody\n"))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if fields["name"] != "go-style" || fields["description"] != "quoted value" || fields["license"] != "MIT" {
		t.Errorf("fields = %v", fields)
	}

	if _, ok, _ := Frontmatter(writeSkill(t, "# no frontmatter\n")); ok {
		t.Error("missing block must give ok=false")
	}
	if _, ok, _ := Frontmatter(writeSkill(t, "---\nname: x\n")); ok {
		t.Error("unterminated block must give ok=false")
	}
}

func TestFrontmatterBlock(t *testing.T) {
	block, ok := FrontmatterBlock([]byte("---\nname: go-style\ndescription: \"quoted value\"\n---\nbody\n"))
	if !ok {
		t.Fatal("ok=false for a terminated block")
	}
	if got, want := string(block), "name: go-style\ndescription: \"quoted value\""; got != want {
		t.Errorf("block = %q, want %q", got, want)
	}

	// Nested/multiline content Frontmatter would drop is preserved verbatim.
	nested := "---\nmeta:\n  - a\n  - b\nallowed: |\n  line1\n  line2\n---\nbody"
	block, ok = FrontmatterBlock([]byte(nested))
	if !ok {
		t.Fatal("ok=false for nested block")
	}
	if got, want := string(block), "meta:\n  - a\n  - b\nallowed: |\n  line1\n  line2"; got != want {
		t.Errorf("nested block = %q, want %q", got, want)
	}

	if _, ok := FrontmatterBlock([]byte("# no frontmatter\n")); ok {
		t.Error("missing block must give ok=false")
	}
	if _, ok := FrontmatterBlock([]byte("---\nname: x\n")); ok {
		t.Error("unterminated block must give ok=false")
	}
	// An empty frontmatter block (immediate closing fence) is still a block.
	if block, ok := FrontmatterBlock([]byte("---\n---\nbody")); !ok || string(block) != "" {
		t.Errorf("empty block: ok=%v block=%q", ok, block)
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"a\n", 1},
		{"a\nb", 2},
		{"a\r\nb\rc\n", 3},
	}
	for _, tt := range tests {
		if got := len(SplitLines(tt.in)); got != tt.want {
			t.Errorf("SplitLines(%q) = %d lines, want %d", tt.in, got, tt.want)
		}
	}
}

func FuzzFrontmatter(f *testing.F) {
	f.Add("---\nname: x\ndescription: y\n---\nbody")
	f.Add("---\nname: 'quoted'\n---")
	f.Add("\r\n---\r\nkey: value\r\n---\r\n")
	f.Add("no frontmatter at all")
	f.Add("---\nunterminated")
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, content string) {
		path := filepath.Join(dir, "SKILL.md")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Skip()
		}
		fields, ok, err := Frontmatter(path)
		if err != nil {
			t.Skip() // I/O errors are not parser bugs
		}
		if ok && fields == nil {
			t.Error("ok with nil fields")
		}
		if !ok && fields != nil {
			t.Error("not ok with non-nil fields")
		}
	})
}
