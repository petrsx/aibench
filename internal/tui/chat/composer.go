// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

// The composer: the message textarea, the Enter→SendMsg path, and the input
// history — previously sent messages, browsable with up/down at the textarea's
// first/last line. The current draft is stashed past the newest entry so
// walking back and forward restores it.

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/petrsx/aibench/internal/preset"
)

// newTextarea builds a textarea in the app's style: themed prompt, no bright
// cursor-line background bar.
func newTextarea(placeholder string) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = placeholder
	ta.Prompt = "┃ "
	// The input grows with its content (the chat pane above yields the rows;
	// SetSize re-carves the column when the height changes).
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = 5
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	s := textarea.DefaultDarkStyles()
	s.Focused.CursorLine = lipgloss.NewStyle()
	s.Blurred.CursorLine = lipgloss.NewStyle()
	s.Focused.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("62"))
	s.Blurred.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	ta.SetStyles(s)
	return ta
}

// SetStarters seeds the input history's starter segment with the
// profile's starter prompts (config `prompt.starters:`); they sit before
// the session's own sent messages and stay browsable after use. Called on
// start, profile switch, and starters-file reload; the browse position
// resets past the newest entry.
func (m *Model) SetStarters(starters []preset.Starter) {
	m.starters = starters
	m.histIdx = m.histLen()
}

// histAt returns entry i of the combined history: the starter segment
// first, then the session's sent messages.
func (m *Model) histAt(i int) string {
	if i < len(m.starters) {
		return m.starters[i].Text
	}
	return m.sent[i-len(m.starters)]
}

// histLen is the combined history length; histIdx == histLen means the
// draft position past the newest entry.
func (m *Model) histLen() int { return len(m.starters) + len(m.sent) }

// resize re-carves the column around a mutation that can change the
// input's dynamic height. Update syncs the generic edit path afterwards,
// but every path that sets or clears the composer wholesale — sending,
// history recall, a starter pick — returns before reaching it, and a
// stale height leaves the transcript sized for a box that is no longer
// that tall.
func (m *Model) resize(mutate func()) {
	before := m.composerHeight()
	mutate()
	m.syncInputHeight(before)
}

// Insert replaces the composer draft with text (a starter-prompt pick),
// cursor at the end — editable before sending, like a history recall.
func (m *Model) Insert(text string) {
	m.resize(func() {
		m.input.SetValue(text)
		m.input.MoveToEnd()
	})
	m.histIdx = m.histLen()
}

// send does the input/history bookkeeping and emits the SendMsg intent for the
// shell to orchestrate (read the Prompt tab, build messages, drive the request).
func (m *Model) send() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return nil
	}
	m.sent = append(m.sent, text)
	m.histIdx = m.histLen()
	m.draft = ""
	// A multi-line draft collapses to one row here; the column has to be
	// re-carved or the transcript keeps the taller box's rows.
	m.resize(m.input.Reset)
	m.stickBottom = true // your own message should land in view
	return func() tea.Msg { return SendMsg{Text: text} }
}

// histPrev recalls the previous history entry (sent messages first, then
// the preset segment) when the cursor sits on the first line of the
// textarea. The current draft is stashed.
func (m *Model) histPrev() bool {
	if m.histLen() == 0 || m.histIdx == 0 || m.input.Line() != 0 {
		return false
	}
	if m.histIdx == m.histLen() {
		m.draft = m.input.Value()
	}
	m.histIdx--
	m.resize(func() { m.input.SetValue(m.histAt(m.histIdx)) })
	return true
}

// histNext walks forward when the cursor sits on the last line, restoring the
// stashed draft past the newest entry.
func (m *Model) histNext() bool {
	if m.histIdx >= m.histLen() || m.input.Line() != m.input.LineCount()-1 {
		return false
	}
	m.histIdx++
	m.resize(func() {
		if m.histIdx == m.histLen() {
			m.input.SetValue(m.draft)
		} else {
			m.input.SetValue(m.histAt(m.histIdx))
		}
	})
	return true
}
