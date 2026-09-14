// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// The three tabs, cycled with shift+tab or clicked in the tab bar. The
// constants are the registry indices: each tab lives at m.tabs[tab*].
//
// Adding one is a new package satisfying tabModel, an entry in the
// registry below, and a case in the layout — only SetSize stays a switch,
// since the tabs take different geometry.
const (
	tabChat      = iota // chat + latest-record metrics
	tabInspector        // full-screen raw HTTP traffic
	tabPrompt           // system prompt + sampling params
	tabCount
)

// tabModel is the slice of each tab's tea.Model the shell drives uniformly.
// All three tabs already satisfy it (inspector carries no-op Focus/Blur), so
// tab switching, focus, and rendering are table lookups rather than switches.
// SetSize is deliberately absent: the tabs take different geometry, so layout()
// stays an honest per-tab switch. Cursor is the tab's real-terminal-cursor
// position (screen coordinates) for the shell's tea.View, nil to hide it.
//
// ShortHelp/FullHelp are the tab's own keys (the help.KeyMap contract): each tab
// owns and exposes them, and the shell composes them with the global chords —
// so no tab-specific key knowledge lives in the shell.
type tabModel interface {
	Update(tea.Msg) tea.Cmd
	View() string
	Focus() tea.Cmd
	Blur()
	Cursor() *tea.Cursor
	ShortHelp() []key.Binding
	FullHelp() [][]key.Binding
	// CancelFind closes an open find overlay, returning whether it did — so the
	// shell's esc handler can claim esc for the search before its global use.
	CancelFind() bool
	// Tip is the hint the shell shows for this tab, and NextTip turns it
	// over. The shell owns the cadence and the place it is drawn (the tab
	// bar's row, tips.go); each tab owns what its hints say, the same
	// split as the keys.
	Tip() string
	NextTip()
	// Notify shows an app-level message in this tab's own notice spot —
	// see notice.go: the shell decides what to say, the tab in front
	// decides where it reads best.
	Notify(text string) tea.Cmd
}

// tab is one registry entry: the label shown in the tab bar and the model that
// renders it. The array is the single source of truth for tab names — the
// header derives its labels from here rather than keeping its own copy.
type tab struct {
	name  string
	model tabModel
}

// tabNames is the labels for the header, in tab order.
func (m *Model) tabNames() [tabCount]string {
	var names [tabCount]string
	for i, t := range m.tabs {
		names[i] = t.name
	}
	return names
}

// updateTab dispatches a message to the active tab's Update.
func (m *Model) updateTab(msg tea.Msg) tea.Cmd {
	return m.tabs[m.activeTab].model.Update(msg)
}

// handleMouse switches tabs on a tab-bar click, else delegates to the active
// tab (each tab owns its own panes' mouse).
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft &&
		click.Y >= tabBarRow-1 && click.Y <= tabBarRow+1 {
		for i, span := range m.tabSpans {
			if click.X >= span[0] && click.X <= span[1] {
				return m.setTab(i)
			}
		}
	}
	return m.updateTab(msg)
}

// setTab switches the active tab, moving focus to its editable widget.
func (m *Model) setTab(t int) tea.Cmd {
	m.activeTab = t
	for i := range m.tabs {
		m.tabs[i].model.Blur()
	}
	cmd := m.tabs[t].model.Focus()
	m.layout()
	return cmd
}

// layout sizes the active tab within the content region (the terminal minus
// the header and help chrome); each tab lays itself out from there. This stays
// a per-tab switch (not a registry lookup): the tabs take different geometry,
// so SetSize is deliberately not part of tabModel.
func (m *Model) layout() {
	content := m.height - headerRows - m.helpHeight()
	if m.settings != nil {
		m.settings.SetSize(m.width, content)
	}
	switch m.activeTab {
	case tabInspector:
		m.inspector.SetSize(headerRows, m.width, content)
	case tabPrompt:
		m.prompt.SetSize(headerRows, m.width, content)
	default:
		m.chat.SetSize(headerRows, m.width, content)
	}
}
