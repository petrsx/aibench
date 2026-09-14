// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

// The transcript search, driven by the shared find widget (a top-right overlay
// with a query input and clickable prev/next icons): ctrl+f opens it, matches
// highlight in the chat viewport, and enter/ctrl+n / ctrl+p (or the ▲/▼ icons)
// walk them. esc closes, routed by the shell via CancelFind. The walk itself
// lives in find.Pane; what follows is only what searching the transcript
// means.

import (
	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/tui/find"
)

// openFind enters search mode (ctrl+f).
func (m *Model) openFind() tea.Cmd { return m.find.Open() }

// The find.Target methods: the transcript is re-derived rather than restyled,
// so a repaint is a full renderChat — which recomputes the matches itself
// (see the find block there), keeping a streaming reply's highlights aligned.
func (m *Model) FindLines() []string { return m.chatLines }
func (m *Model) FindRepaint()        { m.renderChat(false) }
func (m *Model) FindOffset() int     { return m.chatView.YOffset() }

func (m *Model) FindReveal(mt find.Match) {
	m.chatView.EnsureVisible(mt.Line, mt.Start, mt.End)
}

// CancelFind closes whichever of the tab's transient overlays is open —
// search, or the slash-command menu, which esc dismisses by clearing the
// draft that opened it. False when neither was, so the shell applies its
// global esc handling instead.
func (m *Model) CancelFind() bool {
	if m.cmdMenuOpen() {
		m.cmdCloseMenu()
		return true
	}
	return m.find.Dismiss(m)
}

// ApplyTheme re-renders the transcript so the baked-in highlight styles pick
// up the new palette (the shell calls it when the palette flips light/dark;
// New calls it once at construction).
func (m *Model) ApplyTheme() {
	clear(m.derived) // the cached bodies carry the old palette's colors
	m.renderChat(false)
}
