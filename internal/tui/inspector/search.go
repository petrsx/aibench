// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package inspector

// The dump search, driven by the shared find widget (a top-right overlay with
// a query input and clickable prev/next icons): ctrl+f opens it, matches bake
// into the dump lines (searched over the ANSI-stripped text, so the styled
// JSON dump searches cleanly), and enter/ctrl+n / ctrl+p (or the ▲/▼ icons)
// walk them. esc closes, routed by the shell via CancelFind. The walk itself
// lives in find.Pane; what follows is only what searching the dump means.

import (
	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/tui/find"
)

// openFind enters search mode (ctrl+f).
func (m *Model) openFind() tea.Cmd { return m.find.Open() }

// The find.Target methods. dumpLines stays clean of both overlays, so a
// repaint re-derives the viewport from it — selection first, highlights on
// top (refreshDump), which also restores the plain dump once they are gone.
func (m *Model) FindLines() []string { return m.dumpLines }
func (m *Model) FindRepaint()        { m.refreshDump() }
func (m *Model) FindOffset() int     { return m.dump.YOffset() }

func (m *Model) FindReveal(mt find.Match) {
	m.dump.EnsureVisible(mt.Line, mt.Start, mt.End)
}

// CancelFind leaves search mode and restores the unhighlighted dump; false
// when search wasn't open, so the shell applies its global esc handling
// instead.
func (m *Model) CancelFind() bool { return m.find.Dismiss(m) }
