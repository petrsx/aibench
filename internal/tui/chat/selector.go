// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

import (
	"cmp"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// The selector: the command menu's options stage, replacing the
// composer's input frame wholesale — the Claude Code /model shape: a
// title over a description line (the typed filter takes its place while
// narrowing), a numbered list with ❯ on the cursor row and ✔ on the
// current value, and a footer of the keys. Constant height so filtering
// never makes the composer jump.

// selecting reports whether the options stage is active: the draft names
// a picking command, and the composer's input frame is replaced by the
// selector (viewSelector) until the pick lands or esc clears it.
func (m *Model) selecting() bool {
	stage, _, _ := m.cmdParse()
	return stage == stageOpts
}

// selectorHeight is the selector frame's content rows: title,
// description, a blank, a fixed menuRows of list, a blank, the checkbox
// when the command carries one, and the footer — constant while the
// frame is open, so filtering never makes the composer jump.
func (m *Model) selectorHeight() int {
	if !m.selecting() {
		return 0
	}
	return menuRows + 5
}

// viewSelector renders the options stage inside the composer's frame,
// replacing the input — the Claude Code /model shape: a title, the
// description line (the typed filter takes its place while narrowing), a
// numbered list with ❯ on the cursor row, and a footer of the keys.
// Blank rows pad the list so the frame never resizes.
func (m *Model) viewSelector(width int) string {
	_, c, _ := m.cmdParse()
	rows := m.optMatches()
	var b strings.Builder
	// Every line is hard-truncated: a wrap would grow the frame past the
	// constant height the layout carved out.
	b.WriteString(" " + theme.TitleStyle.Render(truncate(cmp.Or(c.Title, titleCase(c.Name)), width-1)) + "\n")
	if filter := m.selectorFilter(); filter != "" {
		b.WriteString(" " + truncate(filter, width-1) + "\n") // typing feedback where the description was
	} else {
		b.WriteString(" " + theme.DimStyle.Render(truncate(cmp.Or(c.Header, c.Desc), width-1)) + "\n")
	}
	b.WriteString("\n")

	listed := 0
	if len(rows) == 0 {
		b.WriteString(theme.DimStyle.Width(width).Render("   no matches") + "\n")
		listed = 1
	} else {
		if m.cmdIdx >= len(rows) {
			m.cmdIdx = 0
		}
		nameW := 12
		for _, o := range rows {
			nameW = max(nameW, lipgloss.Width(o.Name)+2)
		}
		start := 0
		if m.cmdIdx >= menuRows { // keep the cursor visible
			start = m.cmdIdx - menuRows + 1
		}
		name := lipgloss.NewStyle().Width(nameW)
		for i := start; i < len(rows) && i-start < menuRows; i++ {
			marker := "   "
			if i == m.cmdIdx {
				marker = " ❯ "
			}
			row := truncate(marker+fmt.Sprintf("%d. ", i+1)+name.Render(rows[i].Name)+rows[i].Desc, width)
			if i == m.cmdIdx {
				b.WriteString(theme.SelectionStyle.Width(width).Render(row))
			} else {
				b.WriteString(theme.DimStyle.Width(width).Render(row))
			}
			b.WriteString("\n")
			listed++
		}
	}
	for ; listed < menuRows; listed++ { // constant frame height
		b.WriteString("\n")
	}

	footer := " enter pick · type to filter · esc cancel"
	if m.selectorFilter() == "" && len(rows) > 1 {
		footer = " enter pick · 1-9 pick · type to filter · esc cancel"
	}
	b.WriteString("\n" + theme.DimStyle.Render(footer))
	return strings.TrimRight(b.String(), "\n")
}

// selectorFilter is the raw text typed after the command word, for the
// headline's typing feedback (cmdParse lowercases its matching copy).
func (m *Model) selectorFilter() string {
	_, rest, _ := strings.Cut(strings.TrimPrefix(m.input.Value(), "/"), " ")
	return strings.TrimSpace(rest)
}

// truncate hard-caps a plain row to width runes.
func truncate(s string, width int) string {
	r := []rune(s)
	if width > 0 && len(r) > width {
		return string(r[:width-1]) + "…"
	}
	return s
}

// titleCase capitalizes the command name for the selector headline.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
