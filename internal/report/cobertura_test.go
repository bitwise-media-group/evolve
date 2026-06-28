// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package report

import (
	"bytes"
	"encoding/xml"
	"testing"
)

func TestRenderCoberturaXML(t *testing.T) {
	cov := []SkillCoverage{
		{Plugin: "p", Skill: "s1", SkillMD: "plugins/p/skills/s1/SKILL.md", Lines: 10, Covered: true},
		{Plugin: "p", Skill: "s2", SkillMD: "plugins/p/skills/s2/SKILL.md", Lines: 4, Covered: false},
	}
	data := renderCoberturaXML(cov, 1234)
	if !bytes.HasPrefix(data, []byte(xml.Header)) {
		t.Fatalf("missing XML header:\n%s", data)
	}

	var doc coberturaCoverage
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, data)
	}
	if doc.Timestamp != 1234 || doc.Version != coberturaVersion {
		t.Errorf("root attrs = %+v, want timestamp 1234 and the static version", doc)
	}
	if doc.LinesValid != 14 || doc.LinesCovered != 10 || doc.LineRate != "0.7143" {
		t.Errorf("totals = valid %d covered %d rate %q, want 14/10/0.7143", doc.LinesValid, doc.LinesCovered, doc.LineRate)
	}
	if len(doc.Packages.Package) != 1 || doc.Packages.Package[0].Name != "p" {
		t.Fatalf("packages = %+v, want one named p", doc.Packages.Package)
	}
	classes := doc.Packages.Package[0].Classes.Class
	if len(classes) != 2 {
		t.Fatalf("classes = %+v, want s1 and s2", classes)
	}
	s1 := classes[0]
	if s1.Name != "s1" || s1.Filename != "plugins/p/skills/s1/SKILL.md" || s1.LineRate != "1.0000" {
		t.Errorf("s1 = %+v, want fully covered", s1)
	}
	if len(s1.Lines.Line) != 10 || s1.Lines.Line[0].Hits != 1 || s1.Lines.Line[9].Number != 10 {
		t.Errorf("s1 lines = %+v, want 10 lines all hit", s1.Lines.Line)
	}
	s2 := classes[1]
	if s2.LineRate != "0.0000" || len(s2.Lines.Line) != 4 || s2.Lines.Line[0].Hits != 0 {
		t.Errorf("s2 = %+v, want 4 uncovered lines", s2)
	}
}

// TestRenderCoberturaClampsEmptySkill: a 0-line SKILL.md still yields a
// well-formed class with a single line.
func TestRenderCoberturaClampsEmptySkill(t *testing.T) {
	data := renderCoberturaXML([]SkillCoverage{{Plugin: "p", Skill: "s", SkillMD: "s/SKILL.md", Lines: 0, Covered: false}}, 0)
	var doc coberturaCoverage
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	lines := doc.Packages.Package[0].Classes.Class[0].Lines.Line
	if len(lines) != 1 || lines[0].Number != 1 {
		t.Errorf("clamped lines = %+v, want a single line numbered 1", lines)
	}
}

// TestRenderCoberturaEmpty: no skills is a vacuously fully-covered empty document.
func TestRenderCoberturaEmpty(t *testing.T) {
	var doc coberturaCoverage
	if err := xml.Unmarshal(renderCoberturaXML(nil, 0), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.LineRate != "1" || len(doc.Packages.Package) != 0 {
		t.Errorf("empty coverage = %+v, want rate 1 and no packages", doc)
	}
}

func TestCoberturaTimestamp(t *testing.T) {
	if got := coberturaTimestamp("2026-06-11T12:00:00Z"); got != 1781179200000 {
		t.Errorf("timestamp = %d, want 1781179200000", got)
	}
	if got := coberturaTimestamp(""); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
	if got := coberturaTimestamp("not-a-time"); got != 0 {
		t.Errorf("unparseable = %d, want 0", got)
	}
}
