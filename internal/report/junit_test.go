// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package report

import (
	"bytes"
	"encoding/xml"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/results"
)

func TestRenderJUnitXML(t *testing.T) {
	file := &results.File{Skill: "s", Models: map[string]*results.ModelEntry{
		"anthropic/m1": {
			Triggers: &results.TriggerEntry{Results: []results.TriggerResult{
				{Query: "q-pass", Passed: new(true), AvgRunSeconds: new(1.5)},
				{Query: "q-skip"}, // no verdict → skipped
			}},
			Evals: &results.EvalEntry{Results: []results.EvalResult{
				{ID: "e-pass", Passed: new(true), Timing: &results.Timing{ExecutorDurationSeconds: new(2.0)}},
				{ID: "e-fail", Passed: new(false)},
				{ID: "e-err", RuntimeError: "boom"},
			}},
		},
	}}
	loaded := []pluginFiles{{plugin: layout.Plugin{Name: "p"}, files: []*results.File{file}}}

	data := renderJUnitXML(loaded)
	if !bytes.HasPrefix(data, []byte(xml.Header)) {
		t.Fatalf("missing XML header:\n%s", data)
	}

	var root junitTestsuites
	if err := xml.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, data)
	}
	if root.Tests != 5 || root.Failures != 1 || root.Errors != 1 || root.Skipped != 1 {
		t.Errorf("root totals = %+v, want tests=5 failures=1 errors=1 skipped=1", root)
	}
	if len(root.Suites) != 1 || root.Suites[0].Name != "p/s" {
		t.Fatalf("suites = %+v, want one named p/s", root.Suites)
	}
	suite := root.Suites[0]
	if suite.Tests != 5 || suite.Failures != 1 || suite.Errors != 1 || suite.Skipped != 1 {
		t.Errorf("suite totals = %+v", suite)
	}

	byName := map[string]junitTestcase{}
	for _, c := range suite.Cases {
		byName[c.Name] = c
	}
	if c := byName["q-pass"]; c.Failure != nil || c.Skipped != nil || c.Time != "1.500" {
		t.Errorf("q-pass = %+v, want clean pass at 1.500s", c)
	}
	if byName["q-skip"].Skipped == nil {
		t.Error("q-skip must be marked skipped")
	}
	if byName["e-fail"].Failure == nil {
		t.Error("e-fail must carry a <failure>")
	}
	if c := byName["e-err"]; c.Error == nil || c.Error.Message != "boom" {
		t.Errorf("e-err = %+v, want an <error> carrying the runtime error", c)
	}
	if byName["e-pass"].Classname != "p.s.anthropic/m1.evals" {
		t.Errorf("classname = %q, want p.s.anthropic/m1.evals", byName["e-pass"].Classname)
	}
}

// TestRenderJUnitXMLSkipsEmptySuites: a file whose models hold no cases produces
// no suite (e.g. everything filtered out upstream).
func TestRenderJUnitXMLSkipsEmptySuites(t *testing.T) {
	file := &results.File{Skill: "empty", Models: map[string]*results.ModelEntry{}}
	data := renderJUnitXML([]pluginFiles{{plugin: layout.Plugin{Name: "p"}, files: []*results.File{file}}})
	var root junitTestsuites
	if err := xml.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if len(root.Suites) != 0 || root.Tests != 0 {
		t.Errorf("empty input produced %+v, want no suites", root)
	}
}
