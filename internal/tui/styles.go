// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
)

// Palette. 24-bit cyberdream hues: lipgloss/bubbletea v2 detect the terminal's
// colour profile and downsample truecolor natively, so the styles can stay hex
// and still degrade gracefully on 256/16-colour terminals.
var (
	colWhite  = lipgloss.Color("#FFFFFF")
	colGreen  = lipgloss.Color("#5eff6b") // pass
	colRed    = lipgloss.Color("#ff6e5e") // fail
	colPink   = lipgloss.Color("#ff5ea0")
	colYellow = lipgloss.Color("#f1ff5e") // error / warning
	colGrey   = lipgloss.Color("#7b8496") // pending / muted
	colBlue   = lipgloss.Color("#5ea1ff") // focus / accent
	colTeal   = lipgloss.Color("#5ef1ff")
	colOrange = lipgloss.Color("#ffbd5e") // details

	colFaint = lipgloss.Color("#3c4048") // borders, separators

	// Accents for the dashboard's panel borders, one per pane.
	accentExec    = colPink   // execution (left, never focusable)
	accentRollup  = colGreen  // rollup
	accentRuns    = colTeal   // runs
	accentDetails = colOrange // details
)

// dim darkens a pane colour to 40% brightness for an inactive panel's border;
// active panels keep the bright hue. It reads the colour's channels via RGBA so
// it works for every palette entry regardless of how it was specified.
func dim(c color.Color) color.Color {
	r, g, b, _ := c.RGBA() // 16-bit per channel, alpha-premultiplied
	scale := func(v uint32) int { return int(v>>8) * 40 / 100 }
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", scale(r), scale(g), scale(b)))
}

var (
	// Border + in-border title styles, focused vs blurred.
	borderFocusStyle = lipgloss.NewStyle().Foreground(colBlue)
	borderBlurStyle  = lipgloss.NewStyle().Foreground(colFaint)
	titleFocusStyle  = lipgloss.NewStyle().Bold(true).Foreground(colBlue)
	titleBlurStyle   = lipgloss.NewStyle().Foreground(colGrey)
	countStyle       = lipgloss.NewStyle().Foreground(colGrey)

	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(colBlue)
	mutedStyle    = lipgloss.NewStyle().Foreground(colGrey)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(colBlue)
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(colYellow)

	tabActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(accentRollup)

	passStyle  = lipgloss.NewStyle().Foreground(colGreen)
	failStyle  = lipgloss.NewStyle().Foreground(colRed)
	errStyle   = lipgloss.NewStyle().Foreground(colYellow)
	pendStyle  = lipgloss.NewStyle().Foreground(colGrey)
	footerHint = lipgloss.NewStyle().Foreground(colGrey)
	// runStyle tints the live spinner for the run under test (blue); baselineStyle
	// tints the spinner and label of an eval running its without-skill baseline
	// first (yellow), so the two phases read apart at a glance.
	runStyle      = lipgloss.NewStyle().Foreground(colBlue)
	baselineStyle = lipgloss.NewStyle().Foreground(colYellow)

	buttonStyle = lipgloss.NewStyle().Padding(0, 2).Foreground(colGrey).
			Border(lipgloss.RoundedBorder()).BorderForeground(colFaint)
	buttonActive = lipgloss.NewStyle().Padding(0, 2).Bold(true).Foreground(colWhite).
			Background(colBlue).Border(lipgloss.RoundedBorder()).BorderForeground(colBlue)
)
