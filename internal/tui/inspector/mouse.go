// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package inspector

import (
	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/selection"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// posIn maps screen coordinates to a content position in the given pane —
// this tab's pane geometry over the shared selection.PaneAt.
func (m *Model) SelPosAt(pane, x, y int, clamp bool) (selection.Pos, bool) {
	sideLeft := m.width - m.metricsWidth() + 1
	var p selection.Pane
	switch pane {
	case paneDump:
		// Only the tab header sits above the dump — the content pane is
		// borderless, so there is no border row to skip.
		p = selection.Pane{
			Top: m.top + headRows, Left: 1,
			Width: m.dump.Width(), Height: m.dump.Height(),
			YOffset: m.dump.YOffset(), Lines: m.dumpLines,
		}
	case paneHeaders:
		// The Headers panel opens the right column: its top border, then the
		// title rows, then the content.
		p = selection.Pane{
			Top: m.top + 1 + render.SidePanelTitleRows, Left: sideLeft,
			Width: m.headers.Width(), Height: m.headers.Height(),
			YOffset: m.headers.YOffset(), Lines: m.headersLines,
		}
	case paneSizes:
		// Request size sits below Headers: past that whole box (its content
		// plus the chrome above and the border below) and this box's own.
		p = selection.Pane{
			Top: m.top + 1 + render.SidePanelTitleRows + m.headers.Height() + 1 +
				1 + render.SidePanelTitleRows,
			Left:  sideLeft,
			Width: m.sizes.Width(), Height: m.sizes.Height(),
			YOffset: m.sizes.YOffset(), Lines: m.sizesLines,
		}
	default:
		return selection.Pos{}, false
	}
	return selection.PaneAt(p, x, y, clamp)
}

// copyAt resolves a click on one of the tab's copy controls to the lines
// it hands over. The dump has none — ctrl+y takes the body, and the help
// line says so — so these are the side panels', each in its own title
// row's corner. The boxes' own geometry decides where they are (posIn
// above counts the same rows), so a control cannot drift from the box
// drawing it.
func (m *Model) copyAt(x, y int) ([]string, string, bool) {
	sideLeft := m.width - m.metricsWidth()
	headersTop := m.top
	// A panel's outer height is its viewport plus the two borders and the
	// two title rows.
	sizesTop := headersTop + m.headers.Height() + 2 + render.SidePanelTitleRows
	switch {
	case render.PanelControlCells(sideLeft, headersTop, m.metricsWidth(),
		render.CopyLabel)[0].Hit(x, y):
		return m.headersLines, "headers copied to clipboard", true
	case m.sizes.Height() > 0 &&
		render.PanelControlCells(sideLeft, sizesTop, m.metricsWidth(),
			render.CopyLabel)[0].Hit(x, y):
		return m.sizesLines, "sizes copied to clipboard", true
	}
	return nil, "", false
}

// handleMouse toggles the sub-view on a label click and drag-selects over the
// dump / headers panes; release copies the span to the clipboard.
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		return m.wheel(msg)

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return nil
		}
		// The find overlay's ▲/▼ icons walk the matches when clicked.
		if m.find.Click(m, msg.X-m.findX, msg.Y-m.findY) {
			return nil
		}
		// A copy control takes its whole pane.
		if lines, notice, ok := m.copyAt(msg.X, msg.Y); ok {
			return m.note.Copy(render.CopyText(lines), theme.NoticeStyle.Render(notice))
		}
		// The request/response labels sit on the head box's content line,
		// one row past its top border.
		if msg.Y == m.top+1 {
			for i, span := range m.spans {
				if msg.X >= span[0] && msg.X <= span[1] {
					m.view = i
					m.Refresh()
					return nil
				}
			}
		}
		m.sel.Press(m, []int{paneDump, paneHeaders, paneSizes}, msg.X, msg.Y)

	case tea.MouseMotionMsg:
		m.sel.Motion(m, msg.X, msg.Y)

	case tea.MouseReleaseMsg:
		switch g, _ := m.sel.Release(m, msg.X, msg.Y); g {
		case selection.GestureClick:
			// A plain click is not a selection: it clears one, and leaves
			// the clipboard alone.
			m.Refresh()
		case selection.GestureSelect:
			m.Refresh()
			return m.copy()
		}
	}
	return nil
}

// SelRepaint re-derives the whole tab: the dump, the panels and the
// selection over them all come from one pass (Refresh), so there is nothing
// finer to repaint.
func (m *Model) SelRepaint(int) { m.Refresh() }

// wheel scrolls whichever pane the pointer is over: in the right column, the
// Headers or Request size box depending on the row; else the dump.
func (m *Model) wheel(msg tea.MouseWheelMsg) tea.Cmd {
	up := msg.Button == tea.MouseWheelUp
	if msg.X >= m.width-m.metricsWidth() {
		vp := &m.headers
		// The Headers box ends after its border, title rows, content and
		// closing border; below that is Request size when it is showing.
		if m.sizes.Height() > 0 &&
			msg.Y >= m.top+1+render.SidePanelTitleRows+m.headers.Height()+1 {
			vp = &m.sizes
		}
		if up {
			vp.ScrollUp(3)
		} else {
			vp.ScrollDown(3)
		}
		return nil
	}
	if up {
		m.dump.ScrollUp(3)
	} else {
		m.dump.ScrollDown(3)
	}
	return nil
}

// copy puts the selected span on the clipboard (OSC 52: the terminal owns the
// write, so it also works over SSH) and shows a notice.
func (m *Model) copy() tea.Cmd {
	lines := m.dumpLines
	switch m.sel.Pane {
	case paneHeaders:
		lines = m.headersLines
	case paneSizes:
		lines = m.sizesLines
	}
	if len(lines) == 0 {
		return nil
	}
	return m.note.Copy(selection.Copy(lines, m.sel.Anchor, m.sel.Head),
		theme.NoticeStyle.Render("copied to clipboard"))
}
