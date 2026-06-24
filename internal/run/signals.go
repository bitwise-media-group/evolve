// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package run

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bitwise-media-group/evolve/internal/layout"
	"github.com/bitwise-media-group/evolve/internal/manifest"
)

// Signals are the deterministic, non-blocking companion to Checks. Where a
// Finding fails the run, a Signal is gradient feedback (0–100, higher is
// better) on a skill's design quality: how close its SKILL.md is to the ideal
// size and how concise its prose reads. Everything here is a pure function of
// the file bytes — no model, no network — so the scores are reproducible and
// belong in the deterministic Tier-0 checks tier rather than the LLM-judged
// eval tiers. Ambiguity, which genuinely needs a semantic judge, is left out by
// design.

// Signal is one non-blocking, 0–100 advisory score. Unlike a Finding it never
// fails the run; 100 is best.
type Signal struct {
	Name   string  // stable key, e.g. "size", "conciseness", "sentence-length"
	Score  float64 // 0–100
	Detail string  // human-readable basis, e.g. "312 lines (ideal ≤200, cap 500)"
}

// SkillSignals is the advisory score set for one SKILL.md. Overall is the mean
// of the top-level signals (size, conciseness); the sub-scores that feed
// conciseness follow it in Signals for drill-down.
type SkillSignals struct {
	Skill   string   // repo-relative SKILL.md path
	Overall float64  // composite of the top-level signals
	Signals []Signal // ordered: size, conciseness, then conciseness sub-scores
}

// Score returns the score of the named signal and whether it was present.
func (s SkillSignals) Score(name string) (float64, bool) {
	for _, sig := range s.Signals {
		if sig.Name == name {
			return sig.Score, true
		}
	}
	return 0, false
}

// Detail returns the human-readable basis of the named signal, or "" if absent.
func (s SkillSignals) Detail(name string) string {
	for _, sig := range s.Signals {
		if sig.Name == name {
			return sig.Detail
		}
	}
	return ""
}

// SignalConfig holds the scoring knobs, overridable via .evolve like CheckConfig
// so the rubric can be tuned without forking. The size cap is not here: it
// reuses CheckConfig.MaxSkillLines, the same threshold the hard gate enforces.
type SignalConfig struct {
	Enabled bool // whether `run checks` emits the advisory signals at all

	IdealSkillLines int     // size plateau — full marks at or below this
	SizeExponent    float64 // size falloff curvature; >1 makes the penalty accelerate toward the cap

	SentenceTargetWords  float64 // words/sentence at or below which the subscore is 100
	SentenceCeilingWords float64 // words/sentence at or above which the subscore is 0
	HedgeCeilingPer100   float64 // hedge words per 100 at which the subscore is 0
	RedundancyCeiling    float64 // duplicate-line fraction at which the subscore is 0

	SentenceWeight   float64 // weights for the conciseness composite
	HedgeWeight      float64
	RedundancyWeight float64
}

// DefaultSignalConfig encodes the guidance from issue #27: an ideal SKILL.md is
// ~200 lines, hard-capped at 500 (CheckConfig.MaxSkillLines), with a quadratic
// size falloff so a skill stays comfortable through the low 300s and degrades
// sharply as it approaches the cap.
func DefaultSignalConfig() SignalConfig {
	return SignalConfig{
		Enabled:              true,
		IdealSkillLines:      200,
		SizeExponent:         2.0,
		SentenceTargetWords:  20,
		SentenceCeilingWords: 35,
		HedgeCeilingPer100:   6,
		RedundancyCeiling:    0.15,
		SentenceWeight:       1,
		HedgeWeight:          1,
		RedundancyWeight:     1,
	}
}

// minIdealHeadroomTenths is the share of MaxSkillLines (in tenths) that must
// stay above the ideal so the size signal keeps a usable gradient band. At one
// tenth, the ideal may sit no higher than 90% of the cap.
const minIdealHeadroomTenths = 1

// ValidateSignals reports whether the size-signal thresholds are coherent. The
// ideal SKILL.md size must sit below the hard cap (MaxSkillLines) with enough
// headroom to form a gradient; an ideal at or near the cap collapses the score
// into a step. It returns nil when the config is sound.
func (cfg CheckConfig) ValidateSignals() error {
	ideal, maxLines := cfg.Signals.IdealSkillLines, cfg.MaxSkillLines
	limit := maxLines * (10 - minIdealHeadroomTenths) / 10
	switch {
	case ideal > maxLines:
		return fmt.Errorf(
			"checks.ideal_skill_lines (%d) exceeds checks.max_skill_lines (%d)",
			ideal, maxLines)
	case ideal > limit:
		return fmt.Errorf(
			"checks.ideal_skill_lines (%d) is too close to checks.max_skill_lines (%d); keep it ≤ %d",
			ideal, maxLines, limit)
	}
	return nil
}

// Signals computes the non-blocking skill-quality signals for every SKILL.md in
// the repository, in path order. It is the advisory sibling of Checks: it shares
// the same enumeration but never returns Findings, so callers render its output
// as gradient feedback, never as a failure.
func Signals(repo *layout.Repo, cfg CheckConfig) ([]SkillSignals, error) {
	files, err := skillFiles(repo)
	if err != nil {
		return nil, err
	}
	out := make([]SkillSignals, 0, len(files))
	for _, f := range files {
		s, err := scoreSkill(repo, f, cfg)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// skillFiles enumerates every skills/*/SKILL.md across the layout, mirroring the
// glob checkPlugin uses so signals and findings cover the same set.
func skillFiles(repo *layout.Repo) ([]string, error) {
	var dirs []string
	if repo.Kind == layout.Single {
		dirs = []string{repo.Root}
	} else {
		for _, p := range repo.Plugins {
			dirs = append(dirs, p.Dir)
		}
	}
	var files []string
	for _, d := range dirs {
		m, err := filepath.Glob(filepath.Join(d, "skills", "*", "SKILL.md"))
		if err != nil {
			return nil, err
		}
		files = append(files, m...)
	}
	sort.Strings(files)
	return files, nil
}

// scoreSkill reads one SKILL.md and produces its signal set.
func scoreSkill(repo *layout.Repo, skillMD string, cfg CheckConfig) (SkillSignals, error) {
	data, err := os.ReadFile(skillMD)
	if err != nil {
		return SkillSignals{}, fmt.Errorf("%s: %w", repo.Rel(skillMD), err)
	}
	sc := cfg.Signals
	text := string(data)
	lines := len(manifest.SplitLines(text))

	size := sizeScore(lines, sc.IdealSkillLines, cfg.MaxSkillLines, sc.SizeExponent)
	conc, subs := concisenessScore(text, sc)

	sizeDetail := fmt.Sprintf("%d lines (ideal ≤%d, cap %d)", lines, sc.IdealSkillLines, cfg.MaxSkillLines)
	sigs := make([]Signal, 0, 2+len(subs))
	sigs = append(sigs,
		Signal{Name: "size", Score: size, Detail: sizeDetail},
		Signal{Name: "conciseness", Score: conc, Detail: "composite of sentence length, hedging, redundancy"},
	)
	sigs = append(sigs, subs...)
	return SkillSignals{
		Skill:   repo.Rel(skillMD),
		Overall: (size + conc) / 2,
		Signals: sigs,
	}, nil
}

// sizeScore maps a line count to a 0–100 score: 100 at or below ideal, 0 at or
// above the cap, and a curve between the two governed by exponent. With
// exponent > 1 the penalty starts gently just past the ideal and accelerates
// toward the cap, so the score doubles as an early warning that a skill is
// nearing the hard limit. exponent == 1 is a plain linear ramp.
func sizeScore(lines, ideal, cap int, exponent float64) float64 {
	switch {
	case cap <= ideal:
		// Degenerate config: no room for a gradient, fall back to a step.
		if lines <= ideal {
			return 100
		}
		return 0
	case lines <= ideal:
		return 100
	case lines >= cap:
		return 0
	default:
		t := float64(lines-ideal) / float64(cap-ideal)
		return 100 * (1 - math.Pow(t, exponent))
	}
}

// concisenessScore reads the prose of a SKILL.md and returns a composite score
// plus the three sub-scores that feed it. All three are deliberately weak,
// honest proxies — average sentence length, hedge-word density, and line-level
// redundancy — never a claim about whether the skill is well-written.
func concisenessScore(markdown string, cfg SignalConfig) (float64, []Signal) {
	prose := extractProse(markdown)
	avgSent := avgSentenceWords(prose)
	hedges := hedgeDensity(prose)
	dup := redundancyFraction(markdown)

	sentScore := linScore(avgSent, cfg.SentenceTargetWords, cfg.SentenceCeilingWords)
	hedgeScore := linScore(hedges, 0, cfg.HedgeCeilingPer100)
	redunScore := linScore(dup, 0, cfg.RedundancyCeiling)

	composite := weightedMean(
		[]float64{sentScore, hedgeScore, redunScore},
		[]float64{cfg.SentenceWeight, cfg.HedgeWeight, cfg.RedundancyWeight},
	)
	sentDetail := fmt.Sprintf("%.1f words/sentence (target ≤%.0f)", avgSent, cfg.SentenceTargetWords)
	hedgeDetail := fmt.Sprintf("%.1f hedge words per 100 (ceiling %.0f)", hedges, cfg.HedgeCeilingPer100)
	redunDetail := fmt.Sprintf("%.0f%% duplicate lines (ceiling %.0f%%)", dup*100, cfg.RedundancyCeiling*100)
	subs := []Signal{
		{Name: "sentence-length", Score: sentScore, Detail: sentDetail},
		{Name: "hedging", Score: hedgeScore, Detail: hedgeDetail},
		{Name: "redundancy", Score: redunScore, Detail: redunDetail},
	}
	return composite, subs
}

// markerRE strips leading markdown structure (headings, list bullets, ordered
// list numbers, blockquotes) so prose metrics see sentences, not syntax.
var markerRE = regexp.MustCompile(`^(\s*([#>\-*+]|\d+\.)\s+)+`)

// inlineMarkers removes inline emphasis and code ticks so they don't fragment
// words for the tokenizer.
var inlineMarkers = strings.NewReplacer("`", "", "*", "", "_", "")

// extractProse returns the human-readable prose of a SKILL.md with YAML
// frontmatter, fenced code blocks, and markdown markers removed. It is a
// heuristic stripper, good enough to keep code and syntax out of the verbosity
// metrics.
func extractProse(markdown string) string {
	var b strings.Builder
	inFrontmatter, inCode := false, false
	for i, raw := range manifest.SplitLines(markdown) {
		trimmed := strings.TrimSpace(raw)
		if i == 0 && trimmed == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if trimmed == "---" {
				inFrontmatter = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		b.WriteString(inlineMarkers.Replace(markerRE.ReplaceAllString(trimmed, "")))
		b.WriteByte('\n')
	}
	return b.String()
}

// sentenceRE splits prose into sentences on terminal punctuation followed by
// whitespace. It is intentionally simple — abbreviations like "e.g." may
// over-split — which is acceptable for a verbosity proxy.
var sentenceRE = regexp.MustCompile(`[.!?]+\s+`)

// avgSentenceWords returns the mean words per sentence, or 0 when there is no
// prose (which scores as 100 — no verbosity to penalize).
func avgSentenceWords(prose string) float64 {
	totalWords, sentences := 0, 0
	for _, p := range sentenceRE.Split(strings.TrimSpace(prose), -1) {
		n := len(strings.Fields(p))
		if n == 0 {
			continue
		}
		totalWords += n
		sentences++
	}
	if sentences == 0 {
		return 0
	}
	return float64(totalWords) / float64(sentences)
}

// hedges are vague qualifiers and fillers whose density is a rough proxy for
// padding. The list is deliberately conservative to limit false positives.
var hedges = []string{
	"very", "really", "quite", "basically", "essentially", "simply", "just",
	"actually", "generally", "typically", "usually", "perhaps", "maybe",
	"somewhat", "fairly", "rather", "in order to", "note that", "as needed",
	"as appropriate", "appropriately", "etc", "and so on", "of course",
	"obviously", "clearly", "needless to say",
}

var hedgeRE = regexp.MustCompile(`(?i)\b(` + strings.Join(hedges, "|") + `)\b`)

// hedgeDensity returns hedge-word matches per 100 prose words.
func hedgeDensity(prose string) float64 {
	words := len(strings.Fields(prose))
	if words == 0 {
		return 0
	}
	return float64(len(hedgeRE.FindAllStringIndex(prose, -1))) / float64(words) * 100
}

// minRedundancyLen ignores short/structural lines so the redundancy metric keys
// on repeated substantive content, not blank lines or table separators.
const minRedundancyLen = 12

// redundancyFraction returns the share of substantive lines that are exact
// duplicates of an earlier line.
func redundancyFraction(markdown string) float64 {
	seen := map[string]bool{}
	total, dup := 0, 0
	for _, raw := range manifest.SplitLines(markdown) {
		line := strings.TrimSpace(raw)
		if len(line) < minRedundancyLen {
			continue
		}
		total++
		if seen[line] {
			dup++
		} else {
			seen[line] = true
		}
	}
	if total == 0 {
		return 0
	}
	return float64(dup) / float64(total)
}

// linScore maps value within [good, bad] to a 0–100 score (good→100, bad→0),
// clamped outside the range. It assumes lower values are better (good < bad).
func linScore(value, good, bad float64) float64 {
	if good == bad {
		if value <= good {
			return 100
		}
		return 0
	}
	return clampScore(100*(bad-value)/(bad-good), 0, 100)
}

// weightedMean returns the weighted average of values, or 0 when the weights
// sum to zero.
func weightedMean(values, weights []float64) float64 {
	var sum, wsum float64
	for i, v := range values {
		sum += v * weights[i]
		wsum += weights[i]
	}
	if wsum == 0 {
		return 0
	}
	return sum / wsum
}

func clampScore(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}
