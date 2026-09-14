// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

import (
	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/selection"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// Selection panes.
const (
	paneNone = iota
	paneChat
	paneMetrics
)

// chatTop is the screen row the frameless chat content starts on: past the
// tab header.
func (m *Model) chatTop() int { return m.top + render.TabHeaderRows }

// rightTop is the baseline the right column's mouse math hangs off: the
// content region's first row, exactly. The tab header spans the chat
// column only, so nothing pushes this column down — it opens with the
// Tokens graph's own top border. Counting a row that is not there put
// every selection in the Metrics panel one row out, which is felt as
// having to aim below the row you want.
func (m *Model) rightTop() int { return m.top }

// The Metrics panel's copy control, in its title row's right corner. The transcript has
// none: its column closes with the input box rather than a rule, so a
// control there would have to live in the header among the profile strip
// — and a transcript is the one pane you want a piece of far more often
// than the whole, which dragging already gives you.
func (m *Model) metricsCopyAt(x, y int) bool {
	return render.PanelControlCells(m.chatColWidth(), m.rightTop()+m.tokensBlock(),
		m.metricsWidth(), render.CopyLabel)[0].Hit(x, y)
}

// copyMetrics puts the panel's rows on the clipboard as plain text, in the
// shape they are shown (the cache is already clipped to the panel), minus
// the styling and the trailing blanks that padding leaves.
func (m *Model) copyMetrics() tea.Cmd {
	return m.note.Copy(m.copiedMetricsText(),
		theme.NoticeStyle.Render("metrics copied to clipboard"))
}

// copiedMetricsText is what the icon puts on the clipboard.
func (m *Model) copiedMetricsText() string { return render.CopyText(m.metricsLines) }

// pinFromClick pins the record the clicked line belongs to. Clicks that
// would move the panel somewhere the click did not point are ignored: a
// line owned by no record (the empty space below the transcript) leaves
// the panel alone, and so does clicking the group already pinned —
// releasing it there would jump the panel to the newest record, which is
// not what clicking the block you are reading should do.
func (m *Model) pinFromClick(line int) {
	rec := m.recAt(line)
	if rec < 0 || rec == m.state.Pinned() {
		return
	}
	m.state.Pin(rec)
	m.renderMetrics()
	m.renderChat(false)
}

// recAt is the record owning a chat content line, or -1.
func (m *Model) recAt(line int) int {
	if line < 0 || line >= len(m.lineRec) {
		return -1
	}
	return m.lineRec[line]
}

// resultAt is the tool result whose payload control sits under this cell,
// or -1. The control is the words that say what it does, not the row they
// sit on: the rest of the row is text, and text is for selecting.
func (m *Model) resultAt(pos selection.Pos) int {
	for _, h := range m.resultHits {
		if h.line == pos.Line && pos.Col >= h.col0 && pos.Col < h.col1 {
			return h.id
		}
	}
	return -1
}

// posIn maps screen coordinates to a content position in the given pane —
// this tab's pane geometry over the shared selection.PaneAt.
func (m *Model) SelPosAt(pane, x, y int, clamp bool) (selection.Pos, bool) {
	var p selection.Pane
	switch pane {
	case paneChat:
		p = selection.Pane{
			Top: m.chatTop(), Left: 1,
			Width: m.chatView.Width(), Height: m.chatView.Height(),
			YOffset: m.chatView.YOffset(), Lines: m.chatLines,
		}
	case paneMetrics:
		// The panel is fixed (no viewport), so it never scrolls. Its content
		// starts past the box's top border and the title rows.
		p = selection.Pane{
			Top:   m.rightTop() + m.tokensBlock() + 1 + render.SidePanelTitleRows,
			Left:  m.chatColWidth() + 1,
			Width: m.metricsWidth() - 2, Height: m.metricsRows(),
			Lines: m.metricsLines,
		}
	default:
		return selection.Pos{}, false
	}
	return selection.PaneAt(p, x, y, clamp)
}

// rerenderSel repaints the pane owning the current/just-ended selection. The
// metrics panel needs nothing: its selection is applied at View time.
func (m *Model) SelRepaint(pane int) {
	if pane == paneChat {
		m.renderChat(false)
	}
}

// handleMouse scrolls the pane under the pointer and drag-selects over the
// chat / metrics panes; a plain click on a record line pins its record,
// and release copies a real selection to the clipboard.
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		return m.handleWheel(msg)

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return nil
		}
		// The find overlay's ▲/▼ icons walk the matches when clicked.
		if m.find.Click(m, msg.X-m.findX, msg.Y-m.findY) {
			return nil
		}
		// The Metrics panel's copy control takes the whole panel.
		if m.metricsCopyAt(msg.X, msg.Y) {
			return m.copyMetrics()
		}
		// A Tokens graph bar selects its record, like clicking the record line.
		if id, ok := m.tokenBarAt(msg.X, msg.Y); ok {
			if id != m.state.Pinned() {
				m.state.Pin(id)
				m.renderMetrics()
				m.renderChat(false)
			}
			return nil
		}
		// A click in the input box moves the cursor to that cell.
		if m.clickInput(msg.X, msg.Y) {
			return nil
		}
		m.sel.Press(m, []int{paneChat, paneMetrics}, msg.X, msg.Y)

	case tea.MouseMotionMsg:
		m.sel.Motion(m, msg.X, msg.Y)

	case tea.MouseReleaseMsg:
		switch g, pane := m.sel.Release(m, msg.X, msg.Y); g {
		case selection.GestureClick:
			// A plain click is not a selection: don't clobber the clipboard.
			// In the chat it toggles a result payload on its ⎿ header, else
			// pins/unpins the clicked record.
			if pane == paneChat {
				if id := m.resultAt(m.sel.Head); id >= 0 {
					m.resultOpen[id] = !m.resultOpen[id]
					m.renderChat(false)
				} else {
					m.pinFromClick(m.sel.Head.Line)
				}
			}
			m.SelRepaint(pane)
		case selection.GestureSelect:
			m.SelRepaint(m.sel.Pane)
			return m.copySelection()
		}
	}
	return nil
}

// clickInput moves the input cursor to the clicked cell when the click lands
// inside the input box; false when the click is elsewhere. Row targeting is
// best-effort on soft-wrapped lines.
func (m *Model) clickInput(x, y int) bool {
	top := m.chatTop() + m.chatView.Height() + 1 // past the input box's top border
	if y < top || y >= top+m.input.Height() || x < 1 || x >= m.chatColWidth()-1 {
		return false
	}
	row := y - top
	for m.input.Line() > row {
		prev := m.input.Line()
		m.input.CursorUp()
		if m.input.Line() == prev {
			break
		}
	}
	for m.input.Line() < row {
		prev := m.input.Line()
		m.input.CursorDown()
		if m.input.Line() == prev {
			break
		}
	}
	m.input.SetCursorColumn(max(0, x-1-2)) // pane border + the "┃ " prompt
	return true
}

func (m *Model) handleWheel(msg tea.MouseWheelMsg) tea.Cmd {
	up := msg.Button == tea.MouseWheelUp
	switch {
	case msg.X >= m.chatColWidth():
		// The metrics panel is fixed; nothing to scroll.
	case msg.Y < m.chatTop()+m.chatView.Height(): // the transcript's own rows
		if up {
			m.chatView.ScrollUp(3)
		} else {
			m.chatView.ScrollDown(3)
		}
		// Scrolling away from the bottom takes the view off follow; coming
		// back to the bottom re-arms it.
		m.stickBottom = m.chatView.AtBottom()
	default:
		// Move the cursor line in the textarea; it scrolls once the content
		// exceeds the visible height.
		key := tea.KeyPressMsg{Code: tea.KeyDown}
		if up {
			key = tea.KeyPressMsg{Code: tea.KeyUp}
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(key)
		return cmd
	}
	return nil
}

// copySelection puts the selected span, stripped of styling, on the clipboard
// (OSC 52: the terminal owns the write, so it also works over SSH) and shows
// a notice in the chat corner.
func (m *Model) copySelection() tea.Cmd {
	lines := m.chatLines
	if m.sel.Pane == paneMetrics {
		lines = m.metricsLines
	}
	if len(lines) == 0 {
		return nil
	}
	return m.note.Copy(selection.Copy(lines, m.sel.Anchor, m.sel.Head),
		theme.NoticeStyle.Render("copied to clipboard"))
}
