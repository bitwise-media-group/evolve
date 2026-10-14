// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package agui

import (
	"encoding/json"
	"reflect"
	"testing"
)

func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("decode %s: %v", s, err)
	}
	return v
}

// TestPointerRFC6901 runs the RFC 6901 section 5 examples.
func TestPointerRFC6901(t *testing.T) {
	doc := decode(t, `{
		"foo": ["bar", "baz"], "": 0, "a/b": 1, "c%d": 2, "e^f": 3,
		"g|h": 4, "i\\j": 5, "k\"l": 6, " ": 7, "m~n": 8
	}`)
	tests := []struct {
		ptr  string
		want string
	}{
		{"", ""}, // whole document, checked separately
		{"/foo", `["bar","baz"]`},
		{"/foo/0", `"bar"`},
		{"/", `0`},
		{"/a~1b", `1`},
		{"/c%d", `2`},
		{"/e^f", `3`},
		{"/g|h", `4`},
		{"/i\\j", `5`},
		{"/k\"l", `6`},
		{"/ ", `7`},
		{"/m~0n", `8`},
	}
	for _, tt := range tests {
		p, err := ParsePointer(tt.ptr)
		if err != nil {
			t.Fatalf("ParsePointer(%q): %v", tt.ptr, err)
		}
		got, ok := p.Get(doc)
		if !ok {
			t.Errorf("Get(%q) not found", tt.ptr)
			continue
		}
		if tt.ptr == "" {
			if !reflect.DeepEqual(got, doc) {
				t.Errorf("empty pointer did not resolve to the document")
			}
			continue
		}
		if !reflect.DeepEqual(got, decode(t, tt.want)) {
			t.Errorf("Get(%q) = %v, want %s", tt.ptr, got, tt.want)
		}
		if p.String() != tt.ptr {
			t.Errorf("String() = %q, want %q", p.String(), tt.ptr)
		}
	}
}

func TestPointerEscapes(t *testing.T) {
	// ~01 is the literal "~1": ~1 unescapes before ~0.
	p, err := ParsePointer("/~01")
	if err != nil || len(p) != 1 || p[0] != "~1" {
		t.Errorf("ParsePointer(/~01) = %q, %v; want [~1]", p, err)
	}
	for _, bad := range []string{"foo", "/~", "/~2", "/a~"} {
		if _, err := ParsePointer(bad); err == nil {
			t.Errorf("ParsePointer(%q): want error", bad)
		}
	}
	doc := decode(t, `{"a":[1,2]}`)
	for _, missing := range []string{"/b", "/a/2", "/a/01", "/a/-", "/a/x", "/a/0/z"} {
		p, _ := ParsePointer(missing)
		if _, ok := p.Get(doc); ok {
			t.Errorf("Get(%q) found, want absent", missing)
		}
	}
}

// TestApplyPatchRFC6902 runs the RFC 6902 appendix A examples.
func TestApplyPatchRFC6902(t *testing.T) {
	tests := []struct {
		name, doc, patch, want string
	}{
		{"A.1 add object member", `{"foo":"bar"}`, `[{"op":"add","path":"/baz","value":"qux"}]`,
			`{"baz":"qux","foo":"bar"}`},
		{"A.2 add array element", `{"foo":["bar","baz"]}`, `[{"op":"add","path":"/foo/1","value":"qux"}]`,
			`{"foo":["bar","qux","baz"]}`},
		{"A.3 remove member", `{"baz":"qux","foo":"bar"}`, `[{"op":"remove","path":"/baz"}]`, `{"foo":"bar"}`},
		{"A.4 remove element", `{"foo":["bar","qux","baz"]}`, `[{"op":"remove","path":"/foo/1"}]`,
			`{"foo":["bar","baz"]}`},
		{"A.5 replace", `{"baz":"qux","foo":"bar"}`, `[{"op":"replace","path":"/baz","value":"boo"}]`,
			`{"baz":"boo","foo":"bar"}`},
		{"A.6 move member", `{"foo":{"bar":"baz","waldo":"fred"},"qux":{"corge":"grault"}}`,
			`[{"op":"move","from":"/foo/waldo","path":"/qux/thud"}]`,
			`{"foo":{"bar":"baz"},"qux":{"corge":"grault","thud":"fred"}}`},
		{"A.7 move element", `{"foo":["all","grass","cows","eat"]}`,
			`[{"op":"move","from":"/foo/1","path":"/foo/3"}]`, `{"foo":["all","cows","eat","grass"]}`},
		{"A.8 test success", `{"baz":"qux","foo":["a",2,"c"]}`,
			`[{"op":"test","path":"/baz","value":"qux"},{"op":"test","path":"/foo/1","value":2}]`,
			`{"baz":"qux","foo":["a",2,"c"]}`},
		{"A.10 add nested member", `{"foo":"bar"}`, `[{"op":"add","path":"/child","value":{"grandchild":{}}}]`,
			`{"foo":"bar","child":{"grandchild":{}}}`},
		{"A.14 escape ordering", `{"/":9,"~1":10}`, `[{"op":"test","path":"/~01","value":10}]`, `{"/":9,"~1":10}`},
		{"A.16 append to array", `{"foo":["bar"]}`, `[{"op":"add","path":"/foo/-","value":["abc","def"]}]`,
			`{"foo":["bar",["abc","def"]]}`},
		{"copy", `{"a":{"b":1}}`, `[{"op":"copy","from":"/a","path":"/c"}]`, `{"a":{"b":1},"c":{"b":1}}`},
		{"replace root", `{"a":1}`, `[{"op":"replace","path":"","value":[1]}]`, `[1]`},
		{"add null value", `{}`, `[{"op":"add","path":"/n","value":null}]`, `{"n":null}`},
		{"nested array grow", `{"a":[[1]]}`, `[{"op":"add","path":"/a/0/-","value":2}]`, `{"a":[[1,2]]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyPatch(decode(t, tt.doc), json.RawMessage(tt.patch))
			if err != nil {
				t.Fatalf("ApplyPatch: %v", err)
			}
			if !reflect.DeepEqual(got, decode(t, tt.want)) {
				t.Errorf("got %v, want %s", got, tt.want)
			}
		})
	}
}

func TestApplyPatchFailuresAreAtomic(t *testing.T) {
	tests := []struct{ name, doc, patch string }{
		{"A.9 test failure", `{"baz":"qux"}`, `[{"op":"test","path":"/baz","value":"bar"}]`},
		{"A.12 add to nonexistent target", `{"foo":"bar"}`, `[{"op":"add","path":"/baz/bat","value":"qux"}]`},
		{"A.15 test number vs string", `{"/":9,"~1":10}`, `[{"op":"test","path":"/~01","value":"10"}]`},
		{"remove missing", `{}`, `[{"op":"remove","path":"/x"}]`},
		{"replace missing", `{}`, `[{"op":"replace","path":"/x","value":1}]`},
		{"index out of range", `{"a":[1]}`, `[{"op":"add","path":"/a/5","value":1}]`},
		{"move into own child", `{"a":{"b":{}}}`, `[{"op":"move","from":"/a","path":"/a/b/c"}]`},
		{"unknown op", `{}`, `[{"op":"frob","path":"/x"}]`},
		{"missing value", `{}`, `[{"op":"add","path":"/x"}]`},
		{"missing path", `{}`, `[{"op":"add","value":1}]`},
		{"not an array", `{}`, `{"op":"add"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := decode(t, tt.doc)
			// A first, valid op must not leak into the document when a later one fails.
			patch := `[{"op":"add","path":"/leaked","value":true},` + tt.patch[1:]
			if tt.patch[0] != '[' {
				patch = tt.patch
			}
			if _, err := ApplyPatch(doc, json.RawMessage(patch)); err == nil {
				t.Fatal("want error")
			}
			if !reflect.DeepEqual(doc, decode(t, tt.doc)) {
				t.Errorf("document mutated to %v", doc)
			}
		})
	}
}
