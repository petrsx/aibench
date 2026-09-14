// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

// The key sheet: the Help tab of the app screen (/help opens it, tab
// reaches it from /settings) — an overview of every control, grouped by
// owner and rendered straight from the keymaps and the slash-command
// registry, so the sheet cannot drift from the code. The one-line help
// at the bottom of the screen stays the quick reminder; this is the
// reference. The screen widget (internal/tui/screen) pages it; this
// file only renders the content.

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/petrsx/aibench/internal/tui/find"
	"github.com/petrsx/aibench/internal/tui/theme"
	"github.com/petrsx/aibench/internal/version"
)

// findHelp gets the shared find widget's bindings off a throwaway
// instance (HelpBindings is a pointer method).
func findHelp() []key.Binding {
	f := find.New()
	return f.HelpBindings()
}

// commandHelp renders the registry as help rows: the primary surface,
// listed the way the menu offers it — the session group, a blank row,
// the rest. (A binding whose key is a space renders as the blank row;
// the group renderer drops only fully empty ones.)
func (m *Model) commandHelp() []key.Binding {
	out := make([]key.Binding, 0, len(m.commandList)+1)
	for i, c := range m.commandList {
		if i > 0 && m.commandList[i-1].Session && !c.Session {
			out = append(out, key.NewBinding(key.WithHelp(" ", "")))
		}
		out = append(out, key.NewBinding(key.WithHelp("/"+c.Name, c.Desc)))
	}
	return out
}

// renderHelpBody builds the full keymap overview for the Help tab.
func (m *Model) renderHelpBody() string {
	keyCell := lipgloss.NewStyle().Width(20)
	group := func(title string, cols ...[]key.Binding) string {
		var b strings.Builder
		b.WriteString(theme.NoticeStyle.Render(" "+title+" ") + "\n")
		for _, bindings := range cols {
			for _, kb := range bindings {
				h := kb.Help()
				if h.Key == "" && h.Desc == "" {
					continue
				}
				b.WriteString(" " + theme.HelpKeyStyle.Render(keyCell.Render(h.Key)) +
					theme.HelpDescriptionStyle.Render(h.Desc) + "\n")
			}
		}
		return strings.TrimRight(b.String(), "\n")
	}

	left := lipgloss.JoinVertical(lipgloss.Left,
		group("commands", m.commandHelp()),
		"",
		group("command menu", m.chat.MenuHelp()),
		"",
		group("global", m.keys.full()),
		"",
		// Find is one widget every tab opens, so it belongs beside the
		// global keys rather than under any one tab — and the two columns
		// come out even, which is what keeps the sheet on the screen.
		group("find (ctrl+f)", findHelp()),
	)
	right := lipgloss.JoinVertical(lipgloss.Left,
		group("chat", m.chat.FullHelp()...),
		"",
		group("prompt", m.prompt.FullHelp()...),
		"",
		group("inspector", m.inspector.FullHelp()...),
	)
	sheet := lipgloss.JoinHorizontal(lipgloss.Top, left, "    ", right)

	// The credit — build, author, license — leads the sheet: the pane
	// scrolls, and a line below the fold is one nobody sees. Prose, not
	// keymap rows, hence the inline build rather than the group helper.
	var notice strings.Builder
	for _, line := range version.Notice() {
		notice.WriteString(" " + theme.DimStyle.Render(line) + "\n")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		strings.TrimRight(notice.String(), "\n"), "", sheet)
}
