// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package report

import (
	"encoding/xml"
	"strconv"
	"time"
)

// SkillCoverage is one skill's coverage datum, mirroring run.SkillCoverage so the
// report package renders Cobertura without importing the engine. The caller (cli)
// computes it via run.Coverage and maps it across.
type SkillCoverage struct {
	Plugin  string
	Skill   string
	SkillMD string // repo-relative SKILL.md path
	Lines   int
	Covered bool
}

type coberturaCoverage struct {
	XMLName         xml.Name        `xml:"coverage"`
	LineRate        string          `xml:"line-rate,attr"`
	BranchRate      string          `xml:"branch-rate,attr"`
	LinesCovered    int             `xml:"lines-covered,attr"`
	LinesValid      int             `xml:"lines-valid,attr"`
	BranchesCovered int             `xml:"branches-covered,attr"`
	BranchesValid   int             `xml:"branches-valid,attr"`
	Complexity      string          `xml:"complexity,attr"`
	Version         string          `xml:"version,attr"`
	Timestamp       int64           `xml:"timestamp,attr"`
	Sources         coberturaSource `xml:"sources"`
	Packages        struct {
		Package []coberturaPackage `xml:"package"`
	} `xml:"packages"`
}

type coberturaSource struct {
	Source []string `xml:"source"`
}

type coberturaPackage struct {
	Name       string `xml:"name,attr"`
	LineRate   string `xml:"line-rate,attr"`
	BranchRate string `xml:"branch-rate,attr"`
	Complexity string `xml:"complexity,attr"`
	Classes    struct {
		Class []coberturaClass `xml:"class"`
	} `xml:"classes"`
}

type coberturaClass struct {
	Name       string   `xml:"name,attr"`
	Filename   string   `xml:"filename,attr"`
	LineRate   string   `xml:"line-rate,attr"`
	BranchRate string   `xml:"branch-rate,attr"`
	Complexity string   `xml:"complexity,attr"`
	Methods    struct{} `xml:"methods"`
	Lines      struct {
		Line []coberturaLine `xml:"line"`
	} `xml:"lines"`
}

type coberturaLine struct {
	Number int `xml:"number,attr"`
	Hits   int `xml:"hits,attr"`
}

// coberturaVersion is a static tool tag; keep it literal so golden output is
// stable across releases.
const coberturaVersion = "evolve"

// renderCoberturaXML renders per-skill coverage as a Cobertura document:
// packages are plugins, classes are skills (filename = the repo-relative
// SKILL.md), and every line of a covered skill is hit. An empty or unreadable
// SKILL.md is clamped to a single line so its class is never degenerate.
func renderCoberturaXML(cov []SkillCoverage, timestamp int64) []byte {
	doc := coberturaCoverage{
		BranchRate: "0", Complexity: "0", Version: coberturaVersion, Timestamp: timestamp,
		Sources: coberturaSource{Source: []string{"."}},
	}

	order := []string{}
	byPlugin := map[string]*coberturaPackage{}
	var totalCovered, totalValid int
	for _, sc := range cov {
		pkg := byPlugin[sc.Plugin]
		if pkg == nil {
			order = append(order, sc.Plugin)
			pkg = &coberturaPackage{Name: sc.Plugin, BranchRate: "0", Complexity: "0"}
			byPlugin[sc.Plugin] = pkg
		}
		n := sc.Lines
		if n < 1 {
			n = 1
		}
		hits := 0
		if sc.Covered {
			hits = 1
		}
		class := coberturaClass{
			Name: sc.Skill, Filename: sc.SkillMD, BranchRate: "0", Complexity: "0",
			LineRate: rate(coveredLines(sc.Covered, n), n),
		}
		for i := 1; i <= n; i++ {
			class.Lines.Line = append(class.Lines.Line, coberturaLine{Number: i, Hits: hits})
		}
		pkg.Classes.Class = append(pkg.Classes.Class, class)
		totalValid += n
		totalCovered += coveredLines(sc.Covered, n)
	}

	for _, name := range order {
		pkg := byPlugin[name]
		var pc, pv int
		for _, c := range pkg.Classes.Class {
			pv += len(c.Lines.Line)
			for _, l := range c.Lines.Line {
				pc += l.Hits
			}
		}
		pkg.LineRate = rate(pc, pv)
		doc.Packages.Package = append(doc.Packages.Package, *pkg)
	}

	doc.LinesValid = totalValid
	doc.LinesCovered = totalCovered
	doc.LineRate = rate(totalCovered, totalValid)
	return xmlDocument(doc)
}

func coveredLines(covered bool, n int) int {
	if covered {
		return n
	}
	return 0
}

// rate is a covered/valid ratio formatted to four places; a zero denominator is
// vacuously fully covered ("1"), matching Cobertura readers.
func rate(covered, valid int) string {
	if valid == 0 {
		return "1"
	}
	return strconv.FormatFloat(float64(covered)/float64(valid), 'f', 4, 64)
}

// coberturaTimestamp converts the rollup's latest-run RFC3339 stamp to epoch
// milliseconds, or 0 when it is absent or unparseable (keeping output stable).
func coberturaTimestamp(latestRun string) int64 {
	if latestRun == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, latestRun)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// xmlDocument marshals v with the standard XML header and a trailing newline. The
// report structs are static, so marshaling cannot fail.
func xmlDocument(v any) []byte {
	data, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil
	}
	return append(append([]byte(xml.Header), data...), '\n')
}
