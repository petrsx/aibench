// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package screen is the app screen behind /settings and /help: a
// full-pane surface with two inner tabs — Settings, a list of the app's
// own preferences with each value rotated in place (a switch is a
// two-value rotation), and Help, a pre-rendered key/command sheet. It is
// a leaf widget like find — it holds no app state and knows nothing
// about what a setting means or what the sheet says: the shell hands it
// rows and content, and it reports which row the user turned.
//
// Deliberately not a modal overlay: settings and help are read and
// compared, so they take the whole content region — the tab underneath
// is not something you consult mid-edit.
package screen

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// Row is one setting: a stable Key the shell switches on, the label and
// dim hint shown, and the values it rotates through with the current one
// at Index. Values are display strings — the shell maps them back.
type Row struct {
	Key    string
	Label  string
	Hint   string
	Values []string
	Index  int
	// Section starts a new group above this row — a caption like the key
	// sheet's group headers, separating user settings from system ones.
	// Pure decoration: the cursor walks rows, not sections.
	Section string
}

// Value is the row's current value.
func (r Row) Value() string {
	if len(r.Values) == 0 {
		return ""
	}
	return r.Values[r.Index%len(r.Values)]
}

// Result is what a key press did.
type Result int

const (
	None    Result = iota // nothing to act on
	Changed               // a row was turned; the shell applies + persists it
	Closed                // esc — back to the tabs
)

// The two inner tabs.
const (
	tabSettings = iota
	tabHelp
)

// Model is the open screen.
type Model struct {
	rows   []Row
	note   string // dim subtitle under Settings: where these are stored
	help   string // the Help tab's pre-rendered content
	tab    int
	cursor int
	scroll int // Help tab scroll offset
	width  int
	height int
}

// New builds the screen over rows, with note as the dim subtitle saying
// where the values are kept.
func New(rows []Row, note string) Model { return Model{rows: rows, note: note} }

// SetHelp installs the Help tab's content (shell-rendered; the widget
// only pages it).
func (m *Model) SetHelp(content string) { m.help = content }

// ShowHelp opens on the Help tab (/help); /settings leaves the default.
func (m *Model) ShowHelp() { m.tab = tabHelp }

// SetSize fits the screen to the content region.
func (m *Model) SetSize(width, height int) { m.width, m.height = width, height }

// Select puts the cursor on the row with this key (the editor row when
// /profiles-edit finds nothing declared); an unknown key leaves it where it is.
func (m *Model) Select(rowKey string) {
	for i, r := range m.rows {
		if r.Key == rowKey {
			m.cursor = i
			return
		}
	}
}

// Rows exposes the current rows (the shell reads the turned value back).
func (m Model) Rows() []Row { return m.rows }

// Focused is the row under the cursor.
func (m Model) Focused() Row {
	if len(m.rows) == 0 {
		return Row{}
	}
	return m.rows[m.cursor]
}

// Update handles one key press. Both tabs: esc leaves, tab switches
// between Settings and Help. Settings: ↑/↓ walk the rows, ←/→ (and
// enter/space) rotate the value under the cursor. Help: ↑/↓ scroll.
func (m Model) Update(msg tea.KeyPressMsg) (Model, Result, int) {
	switch msg.String() {
	case "esc":
		return m, Closed, -1
	case "tab", "shift+tab":
		if m.help != "" {
			m.tab = 1 - m.tab
		}
		return m, None, -1
	}
	if m.tab == tabHelp {
		switch msg.String() {
		case "up":
			m.scroll = max(0, m.scroll-1)
		case "down":
			m.scroll++ // clamped against the content in View
		case "pgup":
			m.scroll = max(0, m.scroll-m.pageRows())
		case "pgdown":
			m.scroll += m.pageRows()
		}
		return m, None, -1
	}
	if len(m.rows) == 0 {
		return m, None, -1
	}
	switch msg.String() {
	case "up":
		m.cursor = (m.cursor - 1 + len(m.rows)) % len(m.rows)
	case "down":
		m.cursor = (m.cursor + 1) % len(m.rows)
	case "left":
		return m.rotate(-1)
	case "right", "enter", " ":
		return m.rotate(1)
	}
	return m, None, -1
}

// rotate turns the value under the cursor by step, wrapping.
func (m Model) rotate(step int) (Model, Result, int) {
	rows := make([]Row, len(m.rows))
	copy(rows, m.rows)
	r := &rows[m.cursor]
	if n := len(r.Values); n > 1 {
		r.Index = (r.Index + step + n) % n
	}
	m.rows = rows
	return m, Changed, m.cursor
}

// pageRows is the Help tab's visible content height (the tab bar and its
// blank line off the region).
func (m *Model) pageRows() int { return max(1, m.height-4) }

// View renders the screen: the inner tab bar, then the active tab.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(" " + m.tabBar() + "\n\n")
	if m.tab == tabHelp {
		b.WriteString(m.viewHelp())
	} else {
		b.WriteString(m.viewSettings())
	}
	return lipgloss.NewStyle().Width(m.width).Height(m.height).
		MaxHeight(m.height).Padding(1, 1).Render(strings.TrimRight(b.String(), "\n"))
}

// tabBar renders " Settings │ Help ", the active tab banded.
func (m Model) tabBar() string {
	names := []string{"Settings", "Help"}
	parts := make([]string, len(names))
	for i, n := range names {
		if i == m.tab {
			parts[i] = theme.SelectionStyle.Render(" " + n + " ")
		} else {
			parts[i] = theme.DimStyle.Render(" " + n + " ")
		}
	}
	return strings.Join(parts, " ") + theme.DimStyle.Render("   tab switches · esc closes")
}

// viewSettings is the preference list: one row per setting, label left,
// value in the accent column, hint dim after it.
func (m Model) viewSettings() string {
	labelW, valueW := 22, 18
	var b strings.Builder
	b.WriteString(theme.DimStyle.Render(" "+m.note) + "\n\n")
	label := lipgloss.NewStyle().Width(labelW)
	value := lipgloss.NewStyle().Width(valueW)
	for i, r := range m.rows {
		if r.Section != "" {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(" " + theme.NoticeStyle.Render(" "+r.Section+" ") + "\n")
		}
		marker, labelStyle := "   ", theme.DimStyle
		valueStyle := theme.NoticeStyle
		if i == m.cursor {
			marker, labelStyle = " ❯ ", theme.TitleStyle
			valueStyle = theme.SelectionStyle
		}
		row := marker + labelStyle.Render(label.Render(r.Label)) +
			valueStyle.Render(value.Render(" "+r.Value()))
		if r.Hint != "" {
			row += "  " + theme.HelpDescriptionStyle.Render(r.Hint)
		}
		b.WriteString(row + "\n")
	}
	return b.String()
}

// viewHelp pages the pre-rendered sheet by the scroll offset.
func (m Model) viewHelp() string {
	lines := strings.Split(m.help, "\n")
	avail := m.pageRows()
	scroll := min(m.scroll, max(0, len(lines)-avail))
	end := min(len(lines), scroll+avail)
	return strings.Join(lines[scroll:end], "\n")
}

// HelpBindings are the active tab's keys, for the help line. The keys
// are declared as well as described: the help widget renders only
// bindings that have them.
func (m *Model) HelpBindings() []key.Binding {
	if m.tab == tabHelp {
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "scroll")),
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "settings")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
		}
	}
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "select")),
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "change")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "help")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	}
}
