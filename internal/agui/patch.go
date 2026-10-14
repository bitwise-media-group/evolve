// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package agui

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Pointer is a parsed RFC 6901 JSON Pointer: its unescaped reference tokens.
// The empty pointer (no tokens) refers to the whole document.
type Pointer []string

// ParsePointer parses an RFC 6901 JSON Pointer. "" is the whole document;
// anything else must start with "/". "~1" unescapes to "/" and "~0" to "~"
// (in that order, so "~01" is the literal "~1"); any other "~" sequence is
// an error.
func ParsePointer(s string) (Pointer, error) {
	if s == "" {
		return Pointer{}, nil
	}
	if s[0] != '/' {
		return nil, fmt.Errorf("JSON pointer %q must be empty or start with /", s)
	}
	raw := strings.Split(s[1:], "/")
	p := make(Pointer, len(raw))
	for i, tok := range raw {
		for j := 0; j < len(tok); j++ {
			if tok[j] == '~' && (j+1 == len(tok) || (tok[j+1] != '0' && tok[j+1] != '1')) {
				return nil, fmt.Errorf("JSON pointer %q: invalid escape in %q (use ~0 or ~1)", s, tok)
			}
		}
		p[i] = strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
	}
	return p, nil
}

// String renders the pointer back to its escaped RFC 6901 form.
func (p Pointer) String() string {
	var b strings.Builder
	for _, tok := range p {
		b.WriteByte('/')
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(tok, "~", "~0"), "/", "~1"))
	}
	return b.String()
}

// Get resolves the pointer against doc, a value decoded by encoding/json into
// any (maps, slices, and scalars). ok is false when any token is absent.
func (p Pointer) Get(doc any) (v any, ok bool) {
	v = doc
	for _, tok := range p {
		switch node := v.(type) {
		case map[string]any:
			if v, ok = node[tok]; !ok {
				return nil, false
			}
		case []any:
			i, err := arrayIndex(tok, len(node), false)
			if err != nil {
				return nil, false
			}
			v = node[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// patchOp is one RFC 6902 operation.
type patchOp struct {
	Op    string          `json:"op"`
	Path  *string         `json:"path"`
	From  *string         `json:"from"`
	Value json.RawMessage `json:"value"`
}

// ApplyPatch applies an RFC 6902 JSON Patch document to doc and returns the
// patched value. It is atomic: doc is never modified, and on any failing
// operation the error is returned with no partial result.
func ApplyPatch(doc any, patch json.RawMessage) (any, error) {
	var ops []patchOp
	if err := json.Unmarshal(patch, &ops); err != nil {
		return nil, fmt.Errorf("invalid JSON Patch: %w", err)
	}
	out := deepCopy(doc)
	for i, op := range ops {
		var err error
		if out, err = applyOp(out, op); err != nil {
			return nil, fmt.Errorf("patch op %d (%s): %w", i, op.Op, err)
		}
	}
	return out, nil
}

func applyOp(doc any, op patchOp) (any, error) {
	if op.Path == nil {
		return nil, errors.New("missing path")
	}
	path, err := ParsePointer(*op.Path)
	if err != nil {
		return nil, err
	}
	switch op.Op {
	case "add", "replace", "test":
		v, err := op.value()
		if err != nil {
			return nil, err
		}
		switch op.Op {
		case "add":
			return addAt(doc, path, v)
		case "replace":
			return replaceAt(doc, path, v)
		}
		if got, ok := path.Get(doc); !ok || !reflect.DeepEqual(got, v) {
			return nil, fmt.Errorf("test failed at %s", path)
		}
		return doc, nil
	case "remove":
		doc, _, err := removeAt(doc, path)
		return doc, err
	case "move", "copy":
		if op.From == nil {
			return nil, errors.New("missing from")
		}
		src, err := ParsePointer(*op.From)
		if err != nil {
			return nil, err
		}
		if op.Op == "copy" {
			v, ok := src.Get(doc)
			if !ok {
				return nil, fmt.Errorf("from %s does not exist", src)
			}
			return addAt(doc, path, deepCopy(v))
		}
		if len(path) > len(src) && reflect.DeepEqual(path[:len(src)], src) {
			return nil, fmt.Errorf("cannot move %s into its own child %s", src, path)
		}
		doc, v, err := removeAt(doc, src)
		if err != nil {
			return nil, err
		}
		return addAt(doc, path, v)
	}
	return nil, fmt.Errorf("unknown op %q", op.Op)
}

// value decodes the operation's value member.
func (op patchOp) value() (any, error) {
	if op.Value == nil {
		return nil, errors.New("missing value")
	}
	var v any
	if err := json.Unmarshal(op.Value, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// replaceAt returns doc with the existing value at path replaced by v.
func replaceAt(doc any, path Pointer, v any) (any, error) {
	if _, ok := path.Get(doc); !ok {
		return nil, fmt.Errorf("path %s does not exist", path)
	}
	if len(path) == 0 {
		return v, nil
	}
	doc, _, err := removeAt(doc, path)
	if err != nil {
		return nil, err
	}
	return addAt(doc, path, v)
}

// addAt returns doc with v added at path: a member set (or replaced) on an
// object, an element inserted into an array ("-" appends), or the whole
// document replaced for the empty path.
func addAt(doc any, path Pointer, v any) (any, error) {
	if len(path) == 0 {
		return v, nil
	}
	parent, ok := path[:len(path)-1].Get(doc)
	if !ok {
		return nil, fmt.Errorf("parent of %s does not exist", path)
	}
	last := path[len(path)-1]
	switch node := parent.(type) {
	case map[string]any:
		node[last] = v
		return doc, nil
	case []any:
		i, err := arrayIndex(last, len(node), true)
		if err != nil {
			return nil, err
		}
		grown := make([]any, 0, len(node)+1)
		grown = append(grown, node[:i]...)
		grown = append(grown, v)
		grown = append(grown, node[i:]...)
		return setAt(doc, path[:len(path)-1], grown)
	}
	return nil, fmt.Errorf("parent of %s is not a container", path)
}

// removeAt returns doc with the value at path removed, and that value.
func removeAt(doc any, path Pointer) (any, any, error) {
	if len(path) == 0 {
		return nil, nil, errors.New("cannot remove the whole document")
	}
	parent, ok := path[:len(path)-1].Get(doc)
	if !ok {
		return nil, nil, fmt.Errorf("path %s does not exist", path)
	}
	last := path[len(path)-1]
	switch node := parent.(type) {
	case map[string]any:
		v, ok := node[last]
		if !ok {
			return nil, nil, fmt.Errorf("path %s does not exist", path)
		}
		delete(node, last)
		return doc, v, nil
	case []any:
		i, err := arrayIndex(last, len(node), false)
		if err != nil {
			return nil, nil, err
		}
		v := node[i]
		shrunk := append(append(make([]any, 0, len(node)-1), node[:i]...), node[i+1:]...)
		doc, err = setAt(doc, path[:len(path)-1], shrunk)
		return doc, v, err
	}
	return nil, nil, fmt.Errorf("path %s does not exist", path)
}

// setAt replaces the value at an existing path (arrays are values in Go, so
// growing or shrinking one means writing it back into its parent).
func setAt(doc any, path Pointer, v any) (any, error) {
	if len(path) == 0 {
		return v, nil
	}
	parent, _ := path[:len(path)-1].Get(doc)
	last := path[len(path)-1]
	switch node := parent.(type) {
	case map[string]any:
		node[last] = v
	case []any:
		i, err := arrayIndex(last, len(node), false)
		if err != nil {
			return nil, err
		}
		node[i] = v
	}
	return doc, nil
}

// arrayIndex parses an array reference token: decimal digits without a
// leading zero, within bounds; "-" (one past the end) only when adding.
func arrayIndex(tok string, n int, adding bool) (int, error) {
	if tok == "-" && adding {
		return n, nil
	}
	if tok == "" || (len(tok) > 1 && tok[0] == '0') || strings.TrimLeft(tok, "0123456789") != "" {
		return 0, fmt.Errorf("invalid array index %q", tok)
	}
	i, err := strconv.Atoi(tok)
	limit := n - 1
	if adding {
		limit = n
	}
	if err != nil || i > limit {
		return 0, fmt.Errorf("array index %s out of range", tok)
	}
	return i, nil
}

// deepCopy clones a decoded JSON value so patches never alias the input.
func deepCopy(v any) any {
	switch node := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(node))
		for k, x := range node {
			out[k] = deepCopy(x)
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i, x := range node {
			out[i] = deepCopy(x)
		}
		return out
	}
	return v
}
