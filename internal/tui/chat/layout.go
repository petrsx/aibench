// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

// Geometry and composition: SetSize carves the panes out of the content
// region, the small helpers below are the shared measurements (mouse.go leans
// on them too), and layout assembles the pane views — each rendered in its own
// file — into the tab.

import (
	"charm.land/lipgloss/v2"

	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// SetSize lays the panes out. top is the screen row the content region begins
// at (for mouse math); content is the region height (header/help excluded).
func (m *Model) SetSize(top, width, content int) {
	// A geometry change re-wraps content, invalidating a kept selection.
	m.sel.Pane, m.sel.Drag = paneNone, false
	m.top, m.width, m.height = top, width, content

	m.input.SetWidth(max(1, m.chatColWidth()-2))
	// The bottom block: the textarea (+ the stage-1 command list above
	// it), or — while an options pick is open — the selector frame that
	// replaces the input wholesale.
	inputHeight := m.input.Height() + 2 + m.cmdMenuHeight() // textarea + border + command menu
	if m.selecting() {
		inputHeight = m.selectorHeight() + 2
	}

	// The transcript is frameless, so the bar rides beside the content on
	// the column's last cell: -3 is the left pad, the gutter that keeps text
	// off the bar, and the bar itself (see render.MainColumn).
	m.chatView.SetWidth(max(1, m.chatColWidth()-3))
	m.chatView.SetHeight(max(1, content-render.TabHeaderRows-inputHeight))
	m.refresh()
}

// refresh re-derives every cached view from the store + progress snapshot.
func (m *Model) refresh() {
	m.renderChat(true)
	m.renderMetrics()
}

// --- geometry -------------------------------------------------------------

// chatColWidth is the left column (chat + input) outer width; metricsWidth
// is the Metrics panel's. Both come from the shared split, so the three
// tabs' columns line up.
func (m *Model) chatColWidth() int {
	main, _ := render.SplitWidths(m.width)
	return main
}

func (m *Model) metricsWidth() int {
	_, side := render.SplitWidths(m.width)
	return side
}

// tokensBlock is the outer height of the Tokens graph pane atop the right
// column; 0 hides it when the column is too short to share.
func (m *Model) tokensBlock() int {
	if m.height < 16 {
		return 0
	}
	return 10 // border 2 + title/legend row 1 + blank row 1 + chart 6
}

// metricsHeight is the rows the Metrics panel occupies: the content region
// less the Tokens graph above it, so the right column ends exactly where
// the left one does. metricsRows is how many of those reach the content —
// the panel's own arithmetic, not this tab's.
func (m *Model) metricsHeight() int { return m.height - m.tokensBlock() }
func (m *Model) metricsRows() int   { return render.SidePanelRows(m.metricsHeight()) }

// --- composition ----------------------------------------------------------

// layout composes the Chat tab: the chat + input column (3/4) beside the
// Metrics panel (1/4).
func (m *Model) layout() string {
	// The chat itself is frameless; a one-column pad keeps it aligned with the
	// bordered panes. The fixed width keeps the metrics panel flush right even
	// while the chat is empty.
	chat := m.chatView.View()
	// The find overlay floats over the transcript's top-right corner. The chat
	// pane sits inside a one-column pad (screen x0 = 1) starting at chatTop().
	if m.find.Active() {
		cw := m.chatView.Width()
		m.findX, m.findY = 1+cw-m.find.Width(), m.chatTop()
		chat = render.WithFind(chat, m.find.View(), cw, m.chatView.Height())
	}
	// A scrollbar rides the transcript's right edge, mirroring the Inspector's
	// panes — continuous position and visible fraction, where the old footer
	// gave a percentage and cost a row.
	chat = render.ScrollBarFor(chat, m.chatView)
	// The notice goes on last, over the finished column: it ends beside
	// the scrollbar rather than a cell or three short of it, where the
	// viewport happens to stop — and never on the rail itself, which is
	// what says where the reader is in what they are reading.
	if note := m.note.Text(); note != "" {
		chat = render.WithNoticeIn(chat, note, lipgloss.Width(chat), lipgloss.Height(chat),
			lipgloss.Width(chat)-render.ScrollBarCols)
	}
	// The slash-command menu sits between the transcript and the input, the
	// rows it takes already carved out of the transcript by SetSize. An
	// options pick replaces the input frame with the selector instead —
	// same border, the composer in a different mode.
	blocks := []string{
		// The shared tab header leads the column, carrying the shell's
		// profile strip.
		render.TabHeader(m.header, "", m.chatColWidth()),
		// The transcript is frameless, so the shared main column gives it the
		// pad that keeps its first line off the tab header; the bottom is the
		// input box's own border.
		render.MainColumn(chat, m.chatView.Width()),
	}
	if m.selecting() {
		sel := lipgloss.NewStyle().Width(max(1, m.chatColWidth()-2)).Render(m.viewSelector(m.chatColWidth() - 2))
		blocks = append(blocks, theme.FocusedBorder.Render(sel))
	} else {
		if menu := m.viewCommandMenu(m.chatColWidth()); menu != "" {
			blocks = append(blocks, menu)
		}
		blocks = append(blocks, theme.FocusedBorder.Render(m.input.View()))
	}
	left := lipgloss.JoinVertical(lipgloss.Left, blocks...)

	right := m.viewMetrics()
	if m.tokensBlock() > 0 {
		right = lipgloss.JoinVertical(lipgloss.Left, m.viewTokens(), right)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
