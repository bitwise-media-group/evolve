// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Palette. Colours are ANSI-256 so they degrade gracefully on limited
// terminals.
var (
	colGreen  = lipgloss.Color("42")  // pass
	colRed    = lipgloss.Color("203") // fail
	colYellow = lipgloss.Color("214") // error / warning
	colGrey   = lipgloss.Color("245") // pending / muted
	colBlue   = lipgloss.Color("39")  // focus / accent
	colFaint  = lipgloss.Color("239") // borders, separators

	// cyberdream accents for the dashboard's panel borders, one per pane.
	colCyberPink   = lipgloss.Color("#ff5ea0") // execution (left, never focusable)
	colCyberGreen  = lipgloss.Color("#5eff6c") // rollup
	colCyberBlue   = lipgloss.Color("#5ec8ff") // runs
	colCyberOrange = lipgloss.Color("#ffbd5e") // details
)

// dim darkens a hex pane colour for an inactive panel's border/title; non-hex
// colours fall back to the faint border grey. Active panels keep the bright hue.
func dim(c lipgloss.Color) lipgloss.Color {
	s := string(c)
	if len(s) != 7 || !strings.HasPrefix(s, "#") {
		return colFaint
	}
	scale := func(a, b int) int64 {
		n, err := strconv.ParseInt(s[a:b], 16, 0)
		if err != nil {
			return 0
		}
		return n * 40 / 100
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", scale(1, 3), scale(3, 5), scale(5, 7)))
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

	tabActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(colCyberGreen)

	passStyle  = lipgloss.NewStyle().Foreground(colGreen)
	failStyle  = lipgloss.NewStyle().Foreground(colRed)
	errStyle   = lipgloss.NewStyle().Foreground(colYellow)
	pendStyle  = lipgloss.NewStyle().Foreground(colGrey)
	footerHint = lipgloss.NewStyle().Foreground(colGrey)

	buttonStyle = lipgloss.NewStyle().Padding(0, 2).Foreground(colGrey).
			Border(lipgloss.RoundedBorder()).BorderForeground(colFaint)
	buttonActive = lipgloss.NewStyle().Padding(0, 2).Bold(true).Foreground(lipgloss.Color("231")).
			Background(colBlue).Border(lipgloss.RoundedBorder()).BorderForeground(colBlue)
)
