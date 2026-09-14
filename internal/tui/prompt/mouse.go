// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package prompt

// The Prompt tab's mouse handling: wheel scrolls the read-only system-prompt
// view, and clicks on the find overlay's ▲/▼ icons walk the matches.

import (
	tea "charm.land/bubbletea/v2"
)

// handleMouse scrolls the system-prompt view on the wheel and wires the find
// overlay's prev/next icons when search is open.
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			m.sysView.ScrollUp(3)
		} else {
			m.sysView.ScrollDown(3)
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && m.copyAt(msg.X, msg.Y) {
			return m.copyPrompt()
		}
		if msg.Button == tea.MouseLeft && m.paramsCopyAt(msg.X, msg.Y) {
			return m.copyParams()
		}
		if msg.Button == tea.MouseLeft {
			m.find.Click(m, msg.X-m.findX, msg.Y-m.findY)
		}
	}
	return nil
}
