// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// renderHeader draws the shell's stable chrome: the wordmark left, the tab bar
// beside it (browser-style: the active label is a pill), and the active tab's
// rotating hint docked right on the tab bar's row — or, while one applies,
// the news that a newer build exists, which takes that slot and a louder
// colour. The build and the project link live on the help line instead
// (credit.go). It is a stateless function — the tab labels come from the shell's registry
// (m.tabNames), activeTab picks the pill, and tip is whatever the tab in
// front has to say — returning the rendered header and each tab label's
// column span so the shell can hit-test clicks.
func renderHeader(width, activeTab int, names [tabCount]string, tip, news string) (string, [tabCount][2]int) {
	logo := lipgloss.NewStyle().Padding(0, 2, 0, 1).Render(theme.TitleStyle.Render(theme.LogoArt))
	logoLines := strings.Split(logo, "\n")

	// Labels ride the wordmark's baseline row: the active one is a
	// theme-background pill, the rest dim. Every label carries the pill's side
	// padding so switching tabs never shifts columns.
	const tabGap = 3
	var spans [tabCount][2]int
	labelsRow := logoLines[1] + strings.Repeat(" ", tabGap)
	col := lipgloss.Width(labelsRow)
	for i, name := range names {
		if i > 0 {
			labelsRow += strings.Repeat(" ", tabGap)
			col += tabGap
		}
		label, style := " "+name+" ", theme.DimStyle
		if i == activeTab {
			style = theme.SelectionStyle.Bold(true)
		}
		spans[i] = [2]int{col, col + len(label) - 1}
		labelsRow += style.Render(label)
		col += len(label)
	}

	// The tab's hint rides the tab bar's row, right-docked: one line, kept
	// out of the tab header where every cell competes with the profile
	// strip. The wordmark's row carries nothing on the right — the space
	// is the tab bar's to grow into.
	//
	// An available update takes the slot for as long as it applies, and
	// takes the colour with it: a hint is worth reading, an update is
	// worth doing, and the two should not look alike. Tips resume when
	// there is nothing to say — which, for an update, means never in this
	// session, and that is the point.
	label, text := "tip:", tip
	labelStyle, textStyle := theme.MetaKeyStyle, theme.DimStyle
	if news != "" {
		label, text = "update:", news
		labelStyle, textStyle = theme.TitleStyle, theme.NoticeStyle
	}
	plain := [2]string{"", label + " " + ansi.Strip(text) + " "}
	styled := [2]string{"", labelStyle.Render(label) + textStyle.Render(" "+text+" ")}

	rows := make([]string, 3)
	for r, left := range []string{logoLines[0], labelsRow} {
		right, plainRight := styled[r], plain[r]
		// A hint the row cannot hold is dropped whole, never truncated:
		// half a tip is worse than none, and the row is the tab bar's.
		if text == "" || lipgloss.Width(left)+lipgloss.Width(plainRight)+2 > width {
			right, plainRight = "", ""
		}
		gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(plainRight))
		rows[r] = left + strings.Repeat(" ", gap) + right
	}
	rows[2] = theme.NoticeStyle.Render(strings.Repeat("─", max(0, width)))
	return strings.Join(rows, "\n"), spans
}
