// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// View composes the frame and declares the terminal features on it (v2 style:
// alt-screen, mouse, and focus reporting are view fields, not program
// options). The active tab supplies the real cursor position, nil hides it.
func (m *Model) View() tea.View {
	if !m.ready {
		return frame("loading…")
	}

	header, spans := renderHeader(m.width, m.activeTab, m.tabNames(),
		m.tabs[m.activeTab].model.Tip(), m.updateNews)
	m.tabSpans = spans
	// The settings screen replaces the tab's pane while it is open (it is
	// a screen, not an overlay: settings are read and compared, not
	// consulted against the tab underneath).
	pane := m.tabs[m.activeTab].model.View()
	if m.settings != nil {
		pane = m.settings.View()
	}
	parts := []string{
		header,
		pane,
		m.helpLine(),
	}
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	v := frame(content)
	v.Cursor = m.tabs[m.activeTab].model.Cursor()
	if m.settings != nil {
		v.Cursor = nil // the screen owns input while open
	}
	v.WindowTitle = m.windowTitle()
	// The OS-level progress indicator (dock/taskbar) mirrors the wait.
	if m.state.Streaming() {
		v.ProgressBar = &tea.ProgressBar{State: tea.ProgressBarIndeterminate}
	}
	return v
}

// windowTitle names the terminal window: the app, the active profile (when
// profiles are configured), and the model.
func (m *Model) windowTitle() string {
	title := "aibench"
	if m.activeProfile != "" {
		title += " · " + m.activeProfile
	}
	if m.cfg.Model != "" {
		title += " · " + m.cfg.Model
	}
	return title
}

// frame wraps content in the app's terminal setup, re-declared every render.
func frame(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true
	// Kitty-protocol key disambiguation, where the terminal supports it:
	// makes chords like shift+enter distinct key events.
	v.KeyboardEnhancements = tea.KeyboardEnhancements{ReportAlternateKeys: true}
	return v
}

// helpHeight is the rows the help footer occupies: one line of main controls,
// or the expanded keymap.
func (m *Model) helpHeight() int { return 1 }

// helpMap adapts the shell to bubbles/help by composing the global chords with
// the active tab's own keys — the tab owns and exposes its keys (ShortHelp /
// FullHelp), so no tab-specific knowledge lives here. The short line keeps only
// the main controls; /help opens the full keymap.
type helpMap struct{ m *Model }

func (h helpMap) ShortHelp() []key.Binding {
	m := h.m
	if m.settings != nil {
		return m.settings.HelpBindings()
	}
	active := m.tabs[m.activeTab].model
	// Streaming changes the tail, not the line: the tab still owns its
	// controls, and says itself what enter means while a reply is
	// arriving.
	if m.state.Streaming() {
		return append(active.ShortHelp(), m.keys.shortStreaming()...)
	}
	return append(active.ShortHelp(), m.keys.shortNormal()...)
}

func (h helpMap) FullHelp() [][]key.Binding {
	m := h.m
	active := m.tabs[m.activeTab].model
	return append(active.FullHelp(), m.keys.full())
}

func (m *Model) helpLine() string {
	pad := lipgloss.NewStyle().Padding(0, 1)
	// The developer build's esc quit arms an invisible window; while it is
	// open the help line says so, in place of the controls and where they
	// are, because it is about the key just pressed. Everything else on
	// the row (the credit lockup below) is unaffected.
	left := pad.Render(theme.HelpStyle.Render("press esc again to exit"))
	if !m.dev || time.Since(m.lastEsc) > escWindow {
		m.hlp.SetWidth(m.width)
		left = pad.Render(m.hlp.View(helpMap{m}))
	}
	// The build and the project link dock at the right end — the row is
	// never full, and what is left of it after the controls is exactly
	// the space they should have. The lockup narrows and then disappears
	// (credit.go); the controls never give up a cell for it.
	room := m.width - lipgloss.Width(left) - 3
	c := credit(room)
	if c == "" {
		return left
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(c) - 1
	return left + strings.Repeat(" ", gap) + theme.DimStyle.Render(c) + " "
}
