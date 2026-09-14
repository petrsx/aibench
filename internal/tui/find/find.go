// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package find is the reusable in-view search widget shared by the tabs: a
// top-right overlay with a query input, a match count, and clickable prev/next
// icons. Each tab embeds one and drives it over its own viewport content —
// like notify, the widget owns the input, the navigation keys, the current
// match, and the icon hit-testing; the tab owns the content search (Search),
// bakes the highlights into its viewport lines (Highlight), and scrolls the
// current match into view. The tab composites View() top-right
// (render.WithOverlayTopRight) and reports clicks back through HitTest.
//
// The highlight styling is applied here, not through the viewport's
// SetHighlights: that API mis-attributes lines on ANSI-styled content (its
// parse walks byte offsets over the stripped text but detects newlines in the
// raw text), and every pane this app searches is styled.
package find

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// Action is what a key or click did to the widget, for the owning tab to act
// on: recompute matches, walk them, or tear the search down.
type Action int

const (
	ActionNone         Action = iota
	ActionQueryChanged        // the query text changed; recompute matches
	ActionNext                // walk to the next match
	ActionPrev                // walk to the previous match
	ActionClose               // leave search mode
)

// keyMap are the widget's own keys, exposed for the owning tab to fold into its
// contextual help — the single source of truth for both matching and help.
type keyMap struct {
	Next  key.Binding
	Prev  key.Binding
	Close key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Next:  key.NewBinding(key.WithKeys("enter", "ctrl+n"), key.WithHelp("enter/ctrl+n", "next match")),
		Prev:  key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "prev match")),
		Close: key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f/esc", "close find")),
	}
}

// The navigation icons, drawn inside the overlay and clickable.
const (
	prevIcon = "▲"
	nextIcon = "▼"
)

// Model is the find widget. The zero value is not ready — use New.
type Model struct {
	input   textinput.Model
	keys    keyMap
	open    bool
	matches int
	cur     int // index of the current match; -1 when there are none

	// Geometry recorded by View for click hit-testing and cursor placement,
	// all block-relative (columns from the overlay's own left border, rows from
	// its top border). The tab adds the overlay's screen origin.
	width    int
	prevSpan [2]int // ▲ column span on the icon row
	nextSpan [2]int // ▼ column span on the icon row
}

// New builds a find widget with an empty, blurred query input.
func New() Model {
	ti := textinput.New()
	ti.Placeholder = "find"
	ti.Prompt = ""
	ti.SetWidth(20)
	ti.CharLimit = 64
	return Model{input: ti, keys: newKeyMap(), cur: -1}
}

// Open enters search mode and focuses the query input.
func (m *Model) Open() tea.Cmd {
	m.open = true
	return m.input.Focus()
}

// Close leaves search mode, clears the query, and drops the match count. The
// tab is responsible for clearing its viewport highlights.
func (m *Model) Close() {
	m.open = false
	m.input.Reset()
	m.input.Blur()
	m.matches = 0
	m.cur = -1
}

// Active reports whether the search overlay is showing.
func (m *Model) Active() bool { return m.open }

// Query is the trimmed search text.
func (m *Model) Query() string { return strings.TrimSpace(m.input.Value()) }

// SetMatches records how many matches the tab found, shown in the overlay,
// and clamps the current match into the new set (dropped when it empties).
func (m *Model) SetMatches(n int) {
	m.matches = n
	if n == 0 {
		m.cur = -1
	} else if m.cur < 0 || m.cur >= n {
		m.cur = 0
	}
}

// Matches is the count last recorded by the tab.
func (m *Model) Matches() int { return m.matches }

// Current is the index of the current match; -1 when there are none.
func (m *Model) Current() int { return m.cur }

// SetCurrent selects a match by index (the tab picks the one nearest the
// view on a query change); out-of-range values are clamped by SetMatches's
// contract, so pass an index from Nearest.
func (m *Model) SetCurrent(i int) {
	if i >= 0 && i < m.matches {
		m.cur = i
	}
}

// Next advances the current match with wrap-around; a no-op with no matches.
func (m *Model) Next() {
	if m.matches > 0 {
		m.cur = (m.cur + 1) % m.matches
	}
}

// Prev walks the current match backwards with wrap-around; a no-op with no
// matches.
func (m *Model) Prev() {
	if m.matches > 0 {
		m.cur = (m.cur - 1 + m.matches) % m.matches
	}
}

// Update routes a key while search is open and returns what it did. A query
// edit returns ActionQueryChanged with the input's command; navigation and
// close keys return their action with no command.
func (m *Model) Update(msg tea.KeyPressMsg) (Action, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Close):
		return ActionClose, nil
	case key.Matches(msg, m.keys.Next):
		return ActionNext, nil
	case key.Matches(msg, m.keys.Prev):
		return ActionPrev, nil
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		return ActionQueryChanged, cmd
	}
	return ActionNone, cmd
}

// HitTest maps a click at block-relative coordinates to a navigation action,
// so the owning tab can wire the ▲/▼ icons to prev/next. Row 1 is the content
// row inside the top border.
func (m *Model) HitTest(relX, relY int) Action {
	if relY != 1 {
		return ActionNone
	}
	switch {
	case relX >= m.prevSpan[0] && relX <= m.prevSpan[1]:
		return ActionPrev
	case relX >= m.nextSpan[0] && relX <= m.nextSpan[1]:
		return ActionNext
	}
	return ActionNone
}

// Width is the rendered overlay's outer width (with border), so the tab can
// anchor it flush right.
func (m *Model) Width() int { return m.width }

// Cursor is the real terminal cursor inside the query input, offset by the
// overlay's screen origin; nil when the search is closed.
func (m *Model) Cursor(originX, originY int) *tea.Cursor {
	if !m.open {
		return nil
	}
	c := m.input.Cursor()
	if c == nil {
		return nil
	}
	c.X += originX + 1 + 1 // left border + one-column inner pad
	c.Y = originY + 1      // content row inside the top border
	return c
}

// HelpBindings is the widget's contextual keymap, for the tab to surface while
// search is open.
func (m *Model) HelpBindings() []key.Binding {
	return []key.Binding{m.keys.Next, m.keys.Prev, m.keys.Close}
}

// View renders the overlay block and records the icon spans for hit-testing.
// The layout is a single content row inside a rounded border: the query input,
// the match count, then the clickable ▲/▼ icons.
func (m *Model) View() string {
	in := m.input.View()

	// The counter shows the position ("1 of 4"), padded to the width of
	// "99 of 99" (which "no match" shares) so the overlay never resizes as
	// typing changes the count.
	pos := ""
	switch {
	case m.Query() == "":
	case m.matches == 0:
		pos = "no match"
	default:
		pos = fmt.Sprintf("%d of %d", m.cur+1, m.matches)
	}
	count := theme.DimStyle.Render(fmt.Sprintf("%-*s", len("99 of 99"), pos))

	prev := theme.MetaKeyStyle.Render(prevIcon)
	next := theme.MetaKeyStyle.Render(nextIcon)

	// Build the row left-to-right, tracking block-relative columns (col 0 is the
	// left border; content opens at col 1) so the recorded icon spans match what
	// lands on screen.
	col := 1 // past the left border
	pad := " "
	col += lipgloss.Width(pad) // leading inner pad
	seg := pad + in
	col += lipgloss.Width(in)
	gap := "   "
	seg += gap
	col += lipgloss.Width(gap)
	seg += count
	col += lipgloss.Width(count)
	seg += gap
	col += lipgloss.Width(gap)

	m.prevSpan = [2]int{col, col + lipgloss.Width(prev) - 1}
	seg += prev
	col += lipgloss.Width(prev)
	seg += " "
	col++
	m.nextSpan = [2]int{col, col + lipgloss.Width(next) - 1}
	seg += next + " "

	block := theme.FocusedBorder.Render(seg)
	m.width = lipgloss.Width(block)
	return block
}

// Match is one occurrence of the query: the content line it sits on and its
// cell span within that line's ANSI-stripped text (end exclusive) — the
// coordinates lipgloss.StyleRanges and the viewport's EnsureVisible speak.
type Match struct {
	Line       int
	Start, End int
}

// Search finds every case-insensitive occurrence of query in the content
// lines, in order. Matching runs per line over the ANSI-stripped text — the
// query input is single-line, so a match can never span lines. Empty query or
// no match yields nil.
func Search(lines []string, query string) []Match {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	var out []Match
	for ln, line := range lines {
		stripped := ansi.Strip(line)
		hay := strings.ToLower(stripped)
		for off := 0; ; {
			i := strings.Index(hay[off:], q)
			if i < 0 {
				break
			}
			s := off + i
			e := s + len(q)
			out = append(out, Match{
				Line:  ln,
				Start: ansi.StringWidth(stripped[:s]),
				End:   ansi.StringWidth(stripped[:e]),
			})
			off = e
		}
	}
	return out
}

// Highlight bakes the search styles into the (styled) content lines: every
// match gets theme.SearchMatchStyle and the sel'th match theme.SelectionStyle. The
// input slice is left untouched so the caller's base lines stay clean.
func Highlight(lines []string, matches []Match, sel int) []string {
	if len(matches) == 0 {
		return lines
	}
	perLine := map[int][]lipgloss.Range{}
	for i, mt := range matches {
		st := theme.SearchMatchStyle
		if i == sel {
			st = theme.SelectionStyle
		}
		perLine[mt.Line] = append(perLine[mt.Line], lipgloss.NewRange(mt.Start, mt.End, st))
	}
	out := make([]string, len(lines))
	copy(out, lines)
	for ln, rs := range perLine {
		if ln >= 0 && ln < len(out) {
			out[ln] = lipgloss.StyleRanges(out[ln], rs...)
		}
	}
	return out
}

// Nearest picks the match a fresh query should select: the first one at or
// below the given top line, wrapping to the first match overall when the view
// is past them all; -1 with no matches.
func Nearest(matches []Match, top int) int {
	for i, mt := range matches {
		if mt.Line >= top {
			return i
		}
	}
	if len(matches) > 0 {
		return 0
	}
	return -1
}
