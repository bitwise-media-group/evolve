// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/layout"
)

// Bundle is one deterministic workspace tarball: identical inputs always
// produce identical bytes, so the sha256 digest doubles as the dedupe key.
type Bundle struct {
	// Digest is the hex sha256 of Data.
	Digest string
	// Data is the gzip tarball.
	Data []byte
}

// bundleFile is one file staged into a bundle.
type bundleFile struct {
	// rel is the bundle-relative path (always forward slashes).
	rel string
	// src is the absolute source path.
	src string
	// mode is the normalized file mode: 0755 when any execute bit was set,
	// 0644 otherwise.
	mode int64
}

// buildBundle renders the staged files as a deterministic gzip tarball:
// sorted entries, zeroed timestamps and ownership, normalized modes,
// symlinks dereferenced (a bundle is content, not filesystem structure).
func buildBundle(files []bundleFile) (*Bundle, error) {
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })

	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("remote: bundle: %w", err)
	}
	tw := tar.NewWriter(zw)
	seen := map[string]bool{}
	for _, f := range files {
		if seen[f.rel] {
			continue
		}
		seen[f.rel] = true
		raw, err := os.ReadFile(f.src) // dereferences symlinks
		if err != nil {
			return nil, fmt.Errorf("remote: bundle %s: %w", f.rel, err)
		}
		if err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     f.rel,
			Size:     int64(len(raw)),
			Mode:     f.mode,
			Uid:      0, Gid: 0,
			Format: tar.FormatPAX,
		}); err != nil {
			return nil, fmt.Errorf("remote: bundle %s: %w", f.rel, err)
		}
		if _, err := tw.Write(raw); err != nil {
			return nil, fmt.Errorf("remote: bundle %s: %w", f.rel, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("remote: bundle: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("remote: bundle: %w", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return &Bundle{Digest: hex.EncodeToString(sum[:]), Data: buf.Bytes()}, nil
}

// stageTree stages every regular file under dir as destPrefix/<rel>,
// skipping results files when excludeResults is set (priors travel in the
// unit spec, keeping digests stable across runs).
func stageTree(dir, destPrefix string, excludeResults bool) ([]bundleFile, error) {
	var files []bundleFile
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Hidden state dirs (.git of a fixture checkout, .evolve) never
			// belong in a bundle.
			if name := d.Name(); name != "." && strings.HasPrefix(name, ".") && path != dir {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if excludeResults && isResultsFile(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			// A symlink: stat the target instead.
			info, err = os.Stat(path)
			if err != nil {
				return err
			}
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			if info, err = os.Stat(path); err != nil {
				return err
			}
		}
		mode := int64(0o644)
		if info.Mode().Perm()&0o111 != 0 {
			mode = 0o755
		}
		files = append(files, bundleFile{rel: destPrefix + "/" + rel, src: path, mode: mode})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("remote: stage %s: %w", dir, err)
	}
	return files, nil
}

// isResultsFile reports whether rel names a results.<ext> file (any depth).
func isResultsFile(rel string) bool {
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	return strings.HasPrefix(base, "results.")
}

// TriggersBundle stages a plugin's triggers workspace: every sibling skill
// (the trigger surface is the whole skill roster) plus each skill's
// triggers spec — results files excluded. One bundle serves every trigger
// unit of the plugin.
func TriggersBundle(p layout.Plugin) (*Bundle, error) {
	files, err := stageTree(p.SkillsDir, "skills", false)
	if err != nil {
		return nil, err
	}
	specs, err := evalSpecFiles(p.EvalsDir, "")
	if err != nil {
		return nil, err
	}
	return buildBundle(append(files, specs...))
}

// EvalsBundle stages one skill's evals workspace: the skill directory plus
// its evals/<skill>/ tree (specs and fixtures; results files excluded).
func EvalsBundle(set layout.EvalSet) (*Bundle, error) {
	var files []bundleFile
	if _, err := os.Stat(set.SkillDir); err == nil {
		skillFiles, err := stageTree(set.SkillDir, "skills/"+set.Skill, false)
		if err != nil {
			return nil, err
		}
		files = append(files, skillFiles...)
	}
	specDir := filepath.Join(set.Plugin.EvalsDir, set.Skill)
	specFiles, err := stageTree(specDir, "evals/"+set.Skill, true)
	if err != nil {
		return nil, err
	}
	return buildBundle(append(files, specFiles...))
}

// evalSpecFiles stages evals/*/triggers.* (skill filter optional): the spec
// documents the pod's loaders read, without fixtures or results.
func evalSpecFiles(evalsDir, onlySkill string) ([]bundleFile, error) {
	entries, err := os.ReadDir(evalsDir)
	if err != nil {
		return nil, fmt.Errorf("remote: stage %s: %w", evalsDir, err)
	}
	var files []bundleFile
	for _, e := range entries {
		if !e.IsDir() || (onlySkill != "" && e.Name() != onlySkill) {
			continue
		}
		skillDir := filepath.Join(evalsDir, e.Name())
		specs, err := os.ReadDir(skillDir)
		if err != nil {
			return nil, fmt.Errorf("remote: stage %s: %w", skillDir, err)
		}
		for _, s := range specs {
			if s.IsDir() || !strings.HasPrefix(s.Name(), "triggers.") {
				continue
			}
			files = append(files, bundleFile{
				rel:  "evals/" + e.Name() + "/" + s.Name(),
				src:  filepath.Join(skillDir, s.Name()),
				mode: 0o644,
			})
		}
	}
	return files, nil
}

// ReadBundle is a test helper: it lists a bundle's entries.
func ReadBundle(b *Bundle) (map[string]string, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b.Data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	tr := tar.NewReader(zr)
	out := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		out[hdr.Name] = string(raw)
	}
}
