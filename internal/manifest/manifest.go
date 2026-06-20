// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// fieldRE matches one top-level scalar frontmatter line: `key: value`.
var fieldRE = regexp.MustCompile(`^([A-Za-z][0-9A-Za-z_-]*):\s*(.*)$`)

// ReadJSON parses path as generic JSON.
func ReadJSON(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return v, nil
}

// Frontmatter returns the top-level scalar fields of a SKILL.md's leading
// `---` block. ok is false when the file has no such block or the block is
// unterminated.
func Frontmatter(path string) (fields map[string]string, ok bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	lines := SplitLines(string(data))
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, false, nil
	}
	fields = map[string]string{}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			return fields, true, nil
		}
		if m := fieldRE.FindStringSubmatch(line); m != nil {
			value := strings.TrimSpace(m[2])
			if len(value) >= 2 && value[0] == value[len(value)-1] && (value[0] == '\'' || value[0] == '"') {
				value = value[1 : len(value)-1]
			}
			fields[m[1]] = value
		}
	}
	return nil, false, nil // unterminated block counts as none
}

// SplitLines splits on \n, \r\n, and \r without producing a trailing empty
// line, mirroring Python's str.splitlines used by the original harness.
func SplitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// PluginName returns the name field of dir/.claude-plugin/plugin.json, or ""
// when the manifest is missing, unreadable, or has no string name.
func PluginName(dir string) string {
	v, err := ReadJSON(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		return ""
	}
	obj, _ := v.(map[string]any)
	name, _ := obj["name"].(string)
	return name
}
