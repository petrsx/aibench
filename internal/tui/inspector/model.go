// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package inspector is the Inspector tab: the full HTTP dump of one record
// (the pinned one, else the latest) split into request/response sub-views,
// with a static record-identity frame over a scrollable headers panel. It is
// an independent tea.Model reading the shared session store; it owns its own
// dump and headers viewports.
package inspector

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/petrsx/aibench/internal/state"
	"github.com/petrsx/aibench/internal/tui/find"
	"github.com/petrsx/aibench/internal/tui/notify"
	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/selection"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// The request/response sub-views, toggled with tab or a label click.
const (
	viewRequest  = iota // what went up: URL, sent headers, sent body
	viewResponse        // what came back: status, headers, body
	viewCount
)

// Selection panes.
const (
	paneNone = iota
	paneDump
	paneHeaders
	paneSizes
)

// headRows is how many rows sit above the dump content: the shared tab
// header (sub-tabs + record identity/URL/timings), with no gap under it —
// the column has none above its closing rule either.
const headRows = render.TabHeaderRows

// Model is the Inspector tab.
type Model struct {
	state *state.State // the live layer; embeds the session store
	keys  keyMap       // this tab's keys — matching and help both read them

	// tipIdx is which hint the header is showing; the shell advances it
	// on its timer (tips.go).
	tipIdx int

	dump    viewport.Model
	headers viewport.Model
	// sizes is the request's per-message breakdown: its own scrollable pane
	// under the headers, because it grows with every turn and would
	// otherwise push the headers out of reach in a shared viewport.
	sizes viewport.Model

	view        int               // active sub-view
	spans       [viewCount][2]int // sub-tab label column spans, for clicks
	shownID     int               // record last rendered; -1 none
	shownView   int               // sub-view last rendered, for scroll reset
	renderedVer int               // store version the panes were last derived at; -1 forces

	dumpLines []string
	// decoded is the pane's reading of the body: off, the wire form; on,
	// the strings that carry JSON or newlines shown as what they hold. It
	// is the pane's choice, not each value's — a body is read one way or
	// the other — and it survives moving between records, because it says
	// how you want to read rather than a fact about one of them.
	decoded      bool
	headersLines []string
	sizesLines   []string
	// hasSizes is whether the shown record yields a breakdown at all; the
	// layout folds the pane away when it does not. sizesTitle is that
	// pane's box title, which carries the message count.
	hasSizes   bool
	sizesTitle string

	// dump search (ctrl+f): the shared find widget shown as a top-right overlay,
	// the matches over dumpLines, and the overlay's screen origin recorded at
	// View time for click/cursor math.
	find         find.Pane
	findX, findY int

	// selection over the dump / headers panes.
	sel selection.State

	note notify.Notification // transient footer notice (e.g. "copied to clipboard")

	// geometry: top is the screen row the tab's content begins (below the
	// shell's header); width is the full terminal width; contentH is the
	// region's height, kept so the right column can be re-split when the
	// sub-view changes.
	top      int
	contentH int
	width    int
}

// New builds the Inspector over the shared store, defaulting to the response
// sub-view (what you usually came to inspect).
func New(st *state.State) *Model {
	return &Model{
		state: st, keys: newKeyMap(), view: viewRequest, shownID: -1, shownView: viewRequest, renderedVer: -1,
		find: find.NewPane(),
	}
}

// Cursor is the real terminal cursor inside the find overlay's query input when
// search mode is open (top-right of the dump); hidden otherwise. The overlay
// origin is recorded by View.
func (m *Model) Cursor() *tea.Cursor {
	return m.find.Cursor(m.findX, m.findY)
}

// step moves the shown record by one, pinning what it lands on: walking
// the session is the Inspector's own motion, and clicking a row in another
// tab to change what this one shows is a long way round. The walk itself
// is the store's (StepPin) — the Chat tab offers the same one, and two
// copies of it would be two chances to disagree about what "previous"
// means.
func (m *Model) step(by int) {
	if m.state.StepPin(by) {
		m.Refresh()
	}
}

// formatLabel names how the body is being read, the way the panel titles
// do: what you are looking at, not what pressing it would do. "render",
// not "json" — the pane opens newline-carrying strings into lines as well
// as decoding JSON, and a label naming one of the two would misdescribe
// the other.
func (m *Model) formatLabel() string {
	if m.decoded {
		return "render"
	}
	return "raw"
}

// Notify shows a pre-styled notice in this tab's own spot until it
// lapses. The shell uses it for app-level messages, handing them to
// whichever tab is in front: the placement is the tab's business, and
// each one already knows where a notice reads best in its layout.
func (m *Model) Notify(text string) tea.Cmd { return m.note.Show(text) }

// Focus/Blur satisfy the tab contract; the Inspector has no editable widget.
func (m *Model) Focus() tea.Cmd { return nil }
func (m *Model) Blur()          {}

// Update handles the Inspector's keys (tab toggles the sub-view, others
// scroll the dump) and mouse (sub-tab clicks, drag-to-copy, wheel).
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case notify.ExpiredMsg:
		return nil
	case tea.KeyPressMsg:
		if m.find.Active() {
			return m.find.Route(msg, m)
		}
		switch {
		case key.Matches(msg, m.keys.Search):
			return m.openFind()
		case key.Matches(msg, m.keys.Prev):
			m.step(-1)
			return nil
		case key.Matches(msg, m.keys.Next):
			m.step(1)
			return nil
		case key.Matches(msg, m.keys.Latest):
			// Release the pin: both tabs follow the newest record again.
			if m.state.Pinned() >= 0 {
				m.state.Pin(-1)
				m.Refresh()
			}
			return nil
		case key.Matches(msg, m.keys.Format):
			m.decoded = !m.decoded
			m.Refresh()
			return nil
		case key.Matches(msg, m.keys.Copy):
			// The body as it arrived, not the pane as drawn: the pane is
			// wrapped and hanging-indented for reading, and pasting that
			// into jq would paste the reading aid too.
			return m.note.Copy(m.shownBody(),
				theme.NoticeStyle.Render("body copied to clipboard"))
		case key.Matches(msg, m.keys.Toggle):
			m.view = (m.view + 1) % viewCount
			m.Refresh()
			return nil
		}
		var cmd tea.Cmd
		m.dump, cmd = m.dump.Update(msg)
		return cmd
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return nil
}

// View renders the dump of the active sub-view: plain request/response labels
// above a bordered pane whose footer tracks the scroll position, with the
// right column stacking the record frame above the scrollable headers panel.
func (m *Model) View() string {
	// Re-derive the panes from the store when it changed since the last render.
	// This replaces the shell's old per-mutation push; the Inspector has no
	// animation, so a version check is enough.
	if v := m.state.Version(); v != m.renderedVer {
		m.Refresh()
		m.renderedVer = v
	}

	names := [viewCount]string{viewRequest: "Request", viewResponse: "Response"}
	order := [viewCount]int{viewRequest, viewResponse}
	const tabGap = 3
	labelsRow := "  "
	col := 2
	for pos, view := range order {
		if pos > 0 {
			labelsRow += strings.Repeat(" ", tabGap)
			col += tabGap
		}
		label, style := " "+names[view]+" ", theme.DimStyle
		if view == m.view {
			style = theme.SelectionStyle.Bold(true)
		}
		m.spans[view] = [2]int{col + 1, col + len(label)} // +1: the pane's one-column left pad
		labelsRow += style.Render(label)
		col += len(label)
	}

	// The copy notice composites over the dump's bottom-right corner (same
	// overlay as the chat); the find overlay docks top-right. The scrollbar
	// rides the right edge — joined after the notice so the notice never
	// covers it — in place of the scroll percentage this box used to carry
	// in a footer row.
	dumpView := m.dump.View()
	// The content floats — no border, as on the Chat tab. The head line
	// (sub-tabs + record identity) and the rule under it are what separate
	// it from the chrome; the scrollbar rides beside the text, since there
	// is no border left to paint it onto. Only panels stay boxed.
	dumpView = render.ScrollBarFor(dumpView, m.dump)
	// The notice goes on last, over the finished column: it ends beside
	// the scrollbar rather than where the viewport stops, and never on
	// the rail itself.
	if note := m.note.Text(); note != "" {
		dumpView = render.WithNoticeIn(dumpView, note, lipgloss.Width(dumpView), lipgloss.Height(dumpView),
			lipgloss.Width(dumpView)-render.ScrollBarCols)
	}
	// The shared tab header bounds the content from above; the rule below
	// gives the column a floor to sit on rather than trailing off into the
	// help line.
	// The rule just closes the column. The two things it used to carry
	// are keys now — ctrl+r reads the body the other way, ctrl+y takes
	// it — and a key that the help line and the tips both name does not
	// need a button in the middle of the pane as well.
	rule := render.FooterControls(max(1, m.dump.Width()+2))
	left := lipgloss.JoinVertical(lipgloss.Left,
		render.TabHeader(m.viewHead(labelsRow), "", m.dump.Width()+3),
		render.MainColumn(
			lipgloss.JoinVertical(lipgloss.Left, dumpView, rule),
			m.dump.Width()),
	)

	// The find overlay floats over the left column's top-right corner; record
	// its screen origin for click/cursor math (the left column is flush left in
	// the tab, so its screen x0 is 0 and y0 is m.top).
	if m.find.Active() {
		leftW := lipgloss.Width(left)
		m.findX, m.findY = leftW-m.find.Width(), m.top
		left = render.WithFind(left, m.find.View(), leftW, lipgloss.Height(left))
	}

	right := render.SidePanelWithBar(" Headers", render.CopyLabel, m.headers)
	if m.sizes.Height() > 0 {
		right = lipgloss.JoinVertical(lipgloss.Left, right,
			render.SidePanelWithBar(m.sizesTitle, render.CopyLabel, m.sizes))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
