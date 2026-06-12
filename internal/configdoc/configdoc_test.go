// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package configdoc

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/tailscale/hujson"
)

// parseExample loads a generated example the same way the CLI would, so the
// examples are proven valid for every format the loader accepts.
func parseExample(t *testing.T, format string, data []byte) *viper.Viper {
	t.Helper()
	if format == "jsonc" {
		std, err := hujson.Standardize(data)
		if err != nil {
			t.Fatalf("standardize jsonc: %v", err)
		}
		data, format = std, "json"
	}
	v := viper.New()
	v.SetConfigType(format)
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return v
}

// TestExamplesRoundTrip parses each generated example and checks that every
// option with a default is set to exactly that default, and every
// behavioral-default option stays unset (commented out).
func TestExamplesRoundTrip(t *testing.T) {
	examples := []struct {
		format string
		data   []byte
	}{
		{"yaml", ExampleYAML()},
		{"jsonc", ExampleJSONC()},
		{"toml", ExampleTOML()},
	}
	for _, ex := range examples {
		t.Run(ex.format, func(t *testing.T) {
			v := parseExample(t, ex.format, ex.data)
			for _, o := range Schema() {
				if o.Value == nil {
					if v.IsSet(o.Key) {
						t.Errorf("%s: set to %v, want commented out", o.Key, v.Get(o.Key))
					}
					continue
				}
				if !v.IsSet(o.Key) {
					t.Errorf("%s: unset, want %v", o.Key, o.Value)
					continue
				}
				var got any
				switch o.Value.(type) {
				case string:
					got = v.GetString(o.Key)
				case int:
					got = v.GetInt(o.Key)
				case bool:
					got = v.GetBool(o.Key)
				case float64:
					got = v.GetFloat64(o.Key)
				case []string:
					got = v.GetStringSlice(o.Key)
				default:
					t.Fatalf("%s: unhandled schema type %T", o.Key, o.Value)
				}
				if !reflect.DeepEqual(got, o.Value) {
					t.Errorf("%s = %#v, want %#v", o.Key, got, o.Value)
				}
			}
			if v.IsSet("providers") {
				t.Errorf("providers: set to %v, want commented out", v.Get("providers"))
			}
		})
	}
}

// TestMarkdownCoversSchema ensures the reference page documents every key.
func TestMarkdownCoversSchema(t *testing.T) {
	md := string(Markdown())
	for _, o := range Schema() {
		if !strings.Contains(md, "`"+o.Key+"`") {
			t.Errorf("markdown is missing option %s", o.Key)
		}
	}
	for _, want := range []string{"providers.<name>.models", ".evolve.yaml", ".evolve.jsonc", ".evolve.toml"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown is missing %q", want)
		}
	}
}
