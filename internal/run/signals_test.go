// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitwise-media-group/evolve/internal/layout"
)

func approx(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.05 {
		t.Errorf("got %.4f, want %.4f", got, want)
	}
}

func TestSizeScore(t *testing.T) {
	const ideal, cap = 200, 500
	tests := []struct {
		name     string
		lines    int
		exponent float64
		want     float64
	}{
		{"below ideal plateaus at 100", 150, 2, 100},
		{"at ideal is 100", 200, 2, 100},
		{"quadratic midpoint", 350, 2, 75}, // t=0.5, 1-0.25
		{"quadratic near cap falls steeply", 450, 2, 30.56},
		{"at cap is 0", 500, 2, 0},
		{"above cap clamps to 0", 600, 2, 0},
		{"linear midpoint", 350, 1, 50}, // exponent 1 is a plain ramp
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			approx(t, sizeScore(tt.lines, ideal, cap, tt.exponent), tt.want)
		})
	}
}

func TestSizeScoreDegenerateConfig(t *testing.T) {
	// cap <= ideal leaves no room for a gradient: fall back to a step.
	approx(t, sizeScore(100, 300, 300, 2), 100)
	approx(t, sizeScore(400, 300, 300, 2), 0)
}

func TestAvgSentenceWords(t *testing.T) {
	tests := []struct {
		name  string
		prose string
		want  float64
	}{
		{"empty prose scores no penalty", "", 0},
		{"two sentences average their words", "One two three. Four five.", 2.5},
		{"trailing fragment counts", "Alpha beta gamma delta", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			approx(t, avgSentenceWords(tt.prose), tt.want)
		})
	}
}

func TestHedgeDensity(t *testing.T) {
	approx(t, hedgeDensity(""), 0)
	approx(t, hedgeDensity("delete the file"), 0)
	approx(t, hedgeDensity("just do it"), 100.0/3)          // one hedge in three words
	approx(t, hedgeDensity("in order to proceed"), 100.0/4) // multiword phrase counts once
}

func TestRedundancyFraction(t *testing.T) {
	md := strings.Join([]string{
		"this is a substantive line",
		"this is a substantive line", // exact duplicate
		"another distinct sentence here",
		"short", // below minRedundancyLen, ignored
		"",      // blank, ignored
	}, "\n")
	approx(t, redundancyFraction(md), 1.0/3) // 1 dup of 3 substantive lines
}

func TestExtractProseStripsFrontmatterAndCode(t *testing.T) {
	md := strings.Join([]string{
		"---",
		"name: demo",
		"description: Use when testing",
		"---",
		"# Heading",
		"Real prose sentence.",
		"```go",
		"codeShouldNotCount()",
		"```",
		"- a bullet point",
	}, "\n")
	prose := extractProse(md)
	if strings.Contains(prose, "codeShouldNotCount") {
		t.Errorf("code leaked into prose: %q", prose)
	}
	if strings.Contains(prose, "name: demo") {
		t.Errorf("frontmatter leaked into prose: %q", prose)
	}
	if !strings.Contains(prose, "Real prose sentence.") {
		t.Errorf("prose dropped real content: %q", prose)
	}
	if strings.Contains(prose, "#") || strings.Contains(prose, "- a bullet") {
		t.Errorf("markdown markers not stripped: %q", prose)
	}
}

func TestLinScore(t *testing.T) {
	tests := []struct {
		name             string
		value, good, bad float64
		want             float64
	}{
		{"at good is 100", 20, 20, 35, 100},
		{"at bad is 0", 35, 20, 35, 0},
		{"midpoint", 27.5, 20, 35, 50},
		{"below good clamps to 100", 10, 20, 35, 100},
		{"above bad clamps to 0", 99, 20, 35, 0},
		{"degenerate good==bad, at threshold", 20, 20, 20, 100},
		{"degenerate good==bad, above threshold", 21, 20, 20, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			approx(t, linScore(tt.value, tt.good, tt.bad), tt.want)
		})
	}
}

func TestWeightedMean(t *testing.T) {
	approx(t, weightedMean([]float64{100, 0}, []float64{1, 1}), 50)
	approx(t, weightedMean([]float64{100, 0}, []float64{3, 1}), 75)
	approx(t, weightedMean([]float64{50, 50}, []float64{0, 0}), 0) // zero weights
}

func TestConcisenessScorePenalizesVerbosity(t *testing.T) {
	cfg := DefaultSignalConfig()
	tight, _ := concisenessScore("Delete the file. Commit the change.", cfg)

	verbose := "Basically, in order to actually delete the file you should " +
		"just very carefully and generally note that it is essentially " +
		"obviously needed and so on and rather clearly required of course."
	loose, _ := concisenessScore(verbose, cfg)

	if !(tight > loose) {
		t.Errorf("expected concise prose to outscore verbose: tight=%.1f loose=%.1f", tight, loose)
	}
}

func TestSignalsEndToEnd(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "e2e", "repos", "single"))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := layout.Detect(root, layout.Auto)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Signals(repo, DefaultCheckConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one scored skill")
	}
	for _, s := range got {
		if len(s.Signals) < 2 || s.Signals[0].Name != "size" || s.Signals[1].Name != "conciseness" {
			t.Errorf("%s: expected size+conciseness leading signals, got %+v", s.Skill, s.Signals)
		}
		inRange(t, s.Skill, "overall", s.Overall)
		for _, sig := range s.Signals {
			inRange(t, s.Skill, sig.Name, sig.Score)
		}
	}
}

func inRange(t *testing.T, skill, name string, score float64) {
	t.Helper()
	if score < 0 || score > 100 {
		t.Errorf("%s: signal %q out of range: %.2f", skill, name, score)
	}
}

func TestDefaultSignalConfigEnabled(t *testing.T) {
	if !DefaultSignalConfig().Enabled {
		t.Error("signals should be enabled by default")
	}
}

func TestValidateSignals(t *testing.T) {
	// Default cap is 500, so the 10% headroom rule allows an ideal up to 450.
	cfg := func(ideal int) CheckConfig {
		c := DefaultCheckConfig()
		c.Signals.IdealSkillLines = ideal
		return c
	}
	tests := []struct {
		name    string
		ideal   int
		wantErr bool
	}{
		{"default is valid", 200, false},
		{"at the headroom limit is valid", 450, false},
		{"just over the limit is too close", 451, true},
		{"equal to the cap is too close", 500, true},
		{"above the cap exceeds", 501, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cfg(tt.ideal).ValidateSignals()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateSignals(ideal=%d) error = %v; wantErr %v", tt.ideal, err, tt.wantErr)
			}
		})
	}
}

func TestSkillSignalsLookup(t *testing.T) {
	s := SkillSignals{Signals: []Signal{
		{Name: "size", Score: 80, Detail: "300 lines"},
		{Name: "conciseness", Score: 60, Detail: "composite"},
	}}
	if score, ok := s.Score("size"); !ok || score != 80 {
		t.Errorf("Score(size) = %.0f, %v; want 80, true", score, ok)
	}
	if d := s.Detail("size"); d != "300 lines" {
		t.Errorf("Detail(size) = %q; want %q", d, "300 lines")
	}
	if _, ok := s.Score("missing"); ok {
		t.Error("Score(missing) reported present")
	}
	if d := s.Detail("missing"); d != "" {
		t.Errorf("Detail(missing) = %q; want empty", d)
	}
}
