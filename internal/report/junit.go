// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package report

import (
	"encoding/xml"
	"strconv"

	"github.com/bitwise-media-group/evolve/internal/results"
)

// JUnit/Cobertura are write-only CI artifacts, never read back, so they bypass
// the encfmt round-trip the EVALUATION rollup uses and render straight to XML.

type junitTestsuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Errors   int              `xml:"errors,attr"`
	Skipped  int              `xml:"skipped,attr"`
	Time     string           `xml:"time,attr"`
	Suites   []junitTestsuite `xml:"testsuite"`
}

type junitTestsuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Errors   int             `xml:"errors,attr"`
	Skipped  int             `xml:"skipped,attr"`
	Time     string          `xml:"time,attr"`
	Cases    []junitTestcase `xml:"testcase"`
}

type junitTestcase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitDetail  `xml:"failure,omitempty"`
	Error     *junitDetail  `xml:"error,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
}

type junitDetail struct {
	Message string `xml:"message,attr,omitempty"`
}

type junitSkipped struct{}

// renderJUnitXML renders the loaded per-skill results as a JUnit test-results
// document: one testcase per eval/trigger case per model, grouped into a
// testsuite per skill. It reads the same loaded files the EVALUATION tables do,
// so it honors the active-models filter already applied to them; a skill with no
// stored results (its file never ran) contributes no suite.
func renderJUnitXML(loaded []pluginFiles) []byte {
	root := junitTestsuites{}
	var totalTime float64
	for _, pf := range loaded {
		for _, file := range pf.files {
			suite := junitTestsuite{Name: pf.plugin.Name + "/" + file.Skill}
			var suiteTime float64
			for _, key := range file.ModelKeys() {
				entry := file.Models[key]
				if entry == nil {
					continue
				}
				if entry.Triggers != nil {
					for _, r := range entry.Triggers.Results {
						tc, sec := triggerTestcase(pf.plugin.Name, file.Skill, key, r)
						suite.Cases = append(suite.Cases, tc)
						suiteTime += sec
					}
				}
				if entry.Evals != nil {
					for _, r := range entry.Evals.Results {
						tc, sec := evalTestcase(pf.plugin.Name, file.Skill, key, r)
						suite.Cases = append(suite.Cases, tc)
						suiteTime += sec
					}
				}
			}
			if len(suite.Cases) == 0 {
				continue
			}
			tallySuite(&suite)
			suite.Time = formatSeconds(suiteTime)
			root.Tests += suite.Tests
			root.Failures += suite.Failures
			root.Errors += suite.Errors
			root.Skipped += suite.Skipped
			totalTime += suiteTime
			root.Suites = append(root.Suites, suite)
		}
	}
	root.Time = formatSeconds(totalTime)
	return xmlDocument(root)
}

// triggerTestcase maps one trigger query result to a testcase and its seconds. A
// trigger with no verdict (Passed nil) is skipped; a false verdict is a failure.
func triggerTestcase(plugin, skill, key string, r results.TriggerResult) (junitTestcase, float64) {
	sec := floatOrZero(r.AvgRunSeconds)
	tc := junitTestcase{
		Name:      r.Query,
		Classname: plugin + "." + skill + "." + key + ".triggers",
		Time:      formatSeconds(sec),
	}
	switch {
	case r.Passed == nil:
		tc.Skipped = &junitSkipped{}
	case !*r.Passed:
		tc.Failure = &junitDetail{Message: "did not trigger as expected"}
	}
	return tc, sec
}

// evalTestcase maps one eval result to a testcase and its seconds. A runtime
// error is an error; a false verdict is a failure; a nil verdict without an error
// is skipped.
func evalTestcase(plugin, skill, key string, r results.EvalResult) (junitTestcase, float64) {
	var sec float64
	if r.Timing != nil {
		sec = floatOrZero(r.Timing.ExecutorDurationSeconds)
	}
	name := r.ID
	if r.Name != "" {
		name = r.Name
	}
	tc := junitTestcase{
		Name:      name,
		Classname: plugin + "." + skill + "." + key + ".evals",
		Time:      formatSeconds(sec),
	}
	switch {
	case r.Passed != nil && *r.Passed:
		// passed
	case r.Passed != nil:
		tc.Failure = &junitDetail{Message: "assertions failed"}
	case r.RuntimeError != "":
		tc.Error = &junitDetail{Message: r.RuntimeError}
	default:
		tc.Skipped = &junitSkipped{}
	}
	return tc, sec
}

// tallySuite fills a suite's tests/failures/errors/skipped from its cases.
func tallySuite(s *junitTestsuite) {
	s.Tests = len(s.Cases)
	for _, c := range s.Cases {
		switch {
		case c.Failure != nil:
			s.Failures++
		case c.Error != nil:
			s.Errors++
		case c.Skipped != nil:
			s.Skipped++
		}
	}
}

func floatOrZero(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func formatSeconds(s float64) string {
	return strconv.FormatFloat(s, 'f', 3, 64)
}
