// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package prompt

// The system-prompt search, driven by the shared find widget (a top-right
// overlay with a query input and clickable prev/next icons): ctrl+f opens it,
// matches highlight in the view, and enter/ctrl+n / ctrl+p (or the ▲/▼ icons)
// walk them. esc closes, routed by the shell via CancelFind. The walk itself
// lives in find.Pane; what follows is only what searching the preview means.

import (
	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/tui/find"
)

// openFind enters search mode (ctrl+f).
func (m *Model) openFind() tea.Cmd { return m.find.Open() }

// The find.Target methods. sysLines stays clean of highlights, so a repaint
// bakes the current ones over it — and, once they are cleared, restores the
// unhighlighted preview by the same path.
func (m *Model) FindLines() []string { return m.sysLines }
func (m *Model) FindOffset() int     { return m.sysView.YOffset() }

func (m *Model) FindRepaint() {
	m.sysView.SetContentLines(find.Highlight(m.sysLines, m.find.Hits(), m.find.Current()))
}

func (m *Model) FindReveal(mt find.Match) {
	m.sysView.EnsureVisible(mt.Line, mt.Start, mt.End)
}

// CancelFind leaves search mode and restores the unhighlighted preview; false
// when search wasn't open, so the shell applies its global esc handling
// instead.
func (m *Model) CancelFind() bool { return m.find.Dismiss(m) }

// ApplyTheme re-renders the markdown for the new palette (the shell calls it
// when the palette flips light/dark; New calls it once at construction).
func (m *Model) ApplyTheme() {
	m.renderPreview()
}
