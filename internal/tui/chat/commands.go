// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

// Slash commands: typing "/" in the composer opens a menu of the app's
// own commands — ↑/↓ cycle, tab completes, enter runs. It is the typed
// twin of the global chords (the shell owns what each command does; this
// file only offers, filters, and reports the pick), and the successor of
// the one hard-coded typed command, "exit".
//
// The menu has two stages, both derived from the draft alone. A bare
// "/pre" lists the commands the prefix still selects. Once the draft
// names a command that takes a selection ("/profiles ", "/sessions b"),
// the same menu lists that command's options — the Claude Code /model
// shape: pick inline, under the composer, no modal. Enter on a command
// that takes a selection advances into it; enter on an option runs the
// command with it.
//
// A draft that starts with "/" but names nothing known is still an
// ordinary message: this is a prompt-testing tool, and "/etc/hosts …"
// must reach the model rather than error.

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// menuRows caps the visible menu; typing more of the name is the way to
// reach the rest.
const menuRows = 12 // the whole registry fits; options lists page

// Command is one entry in the menu: the name without its slash, the
// one-line description shown beside it, and — for commands that pick
// something — the options the second stage offers. Options is consulted
// lazily when the draft reaches the command and cached while it stays
// there, so a listing that reads disk (sessions) runs once per visit,
// not per keystroke.
type Command struct {
	Name    string
	Desc    string
	Options func() []Option
	// Session marks a command that changes what is being tested (the
	// profile, the script, the conversation). The shell lists these
	// first; the bare menu sets them apart with a blank row.
	Session bool
	// Title and Header caption the selector (the Claude Code /model
	// shape): Title is the panel's name ("Select profile"), Header the
	// description line under it — what picking here does, consequences
	// included ("the current session is replaced"). They fall back to a
	// capitalized Name and to Desc.
	Title  string
	Header string
}

// Option is one selectable value in a command's second stage.
type Option struct {
	Name string
	Desc string
}

// CommandMsg is the intent an entered command emits, for the shell to
// run. Args is the picked option's name, or whatever free text followed
// the command.
type CommandMsg struct {
	Name string
	Args string
}

// SetCommands installs the commands the menu offers. The shell owns the
// list — it changes with what is configured (no profiles, no /profiles).
func (m *Model) SetCommands(cmds []Command) {
	m.commands = cmds
	m.cmdIdx = 0
	m.optCmd, m.optList = "", nil
}

// cmdStage is what the current draft means to the menu.
type cmdStage int

const (
	stageNone cmdStage = iota // not a command draft; menu closed
	stageCmds                 // filtering command names
	stageOpts                 // filtering one command's options
)

// cmdParse reads the draft: a single-line "/…" is a command in the
// making. Before the first space the prefix filters commands; after it,
// a resolved command with options has its options filtered by the rest
// (option names may contain spaces — sessions, starter titles — so the
// filter is everything after the command word).
func (m *Model) cmdParse() (stage cmdStage, cmd Command, filter string) {
	draft := m.input.Value()
	if !strings.HasPrefix(draft, "/") || strings.Contains(draft, "\n") {
		return stageNone, Command{}, ""
	}
	name, rest, cut := strings.Cut(draft[1:], " ")
	if !cut {
		return stageCmds, Command{}, strings.ToLower(name)
	}
	for _, c := range m.commands {
		if c.Name == strings.ToLower(name) && c.Options != nil {
			return stageOpts, c, strings.ToLower(strings.TrimSpace(rest))
		}
	}
	return stageNone, Command{}, ""
}

// cmdMatches is the commands the stage-1 prefix still selects.
func (m *Model) cmdMatches() []Command {
	stage, _, filter := m.cmdParse()
	if stage != stageCmds {
		return nil
	}
	var out []Command
	for _, c := range m.commands {
		if strings.HasPrefix(c.Name, filter) {
			out = append(out, c)
		}
	}
	return out
}

// optMatches is the options the stage-2 filter still selects —
// case-insensitive substring over name and description, the way every
// picker here filters.
func (m *Model) optMatches() []Option {
	stage, c, filter := m.cmdParse()
	if stage != stageOpts {
		m.optCmd, m.optList = "", nil
		return nil
	}
	if m.optCmd != c.Name { // first keystroke on this command: list once
		m.optCmd, m.optList = c.Name, c.Options()
	}
	var out []Option
	for _, o := range m.optList {
		if filter == "" || strings.Contains(strings.ToLower(o.Name+" "+o.Desc), filter) {
			out = append(out, o)
		}
	}
	return out
}

// cmdMenuOpen reports whether either stage is on screen claiming keys.
func (m *Model) cmdMenuOpen() bool {
	return len(m.cmdMatches()) > 0 || len(m.optMatches()) > 0
}

// cmdMenuHeight is the rows the stage-1 command list takes from the
// transcript (the options stage lives in the selector frame instead).
func (m *Model) cmdMenuHeight() int {
	matches := m.cmdMatches()
	if m.cmdSpacerAt(matches) >= 0 {
		return min(len(matches)+1, menuRows)
	}
	return min(len(matches), menuRows)
}

// cmdSpacerAt is the index of the first non-session match when the bare
// menu shows both kinds — the blank row goes before it — else -1. A
// filtered list is a search result, and a gap in one reads as a glitch.
func (m *Model) cmdSpacerAt(matches []Command) int {
	if _, _, filter := m.cmdParse(); filter != "" {
		return -1
	}
	for i, c := range matches {
		if !c.Session {
			if i == 0 {
				return -1
			}
			return i
		}
	}
	return -1
}

// cmdCloseMenu dismisses the menu by clearing the draft that opened it
// (esc, routed by the shell via CancelFind).
func (m *Model) cmdCloseMenu() {
	m.cmdIdx = 0
	m.optCmd, m.optList = "", nil
	m.resize(m.input.Reset)
}

// cmdKey handles a key while the menu is open; the bool reports whether
// the menu consumed it.
func (m *Model) cmdKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if cmds := m.cmdMatches(); len(cmds) > 0 {
		switch msg.String() {
		case "up":
			m.cmdIdx = (m.cmdIdx - 1 + len(cmds)) % len(cmds)
			return nil, true
		case "down":
			m.cmdIdx = (m.cmdIdx + 1) % len(cmds)
			return nil, true
		case "tab":
			// Complete the selected name; a command with options moves
			// into them (the space opens stage 2), one without sits
			// ready for free arguments.
			c := cmds[m.cmdIdx%len(cmds)]
			m.resize(func() { m.input.SetValue("/" + c.Name + " ") })
			m.cmdIdx = 0
			return nil, true
		case "enter":
			c := cmds[m.cmdIdx%len(cmds)]
			// A command that picks something opens its options instead
			// of running bare — enter on /profiles lists the profiles,
			// exactly as tab would.
			if c.Options != nil {
				m.resize(func() { m.input.SetValue("/" + c.Name + " ") })
				m.cmdIdx = 0
				if m.cmdMenuOpen() {
					return nil, true
				}
				// No options right now (no saved sessions yet): run
				// bare and let the shell say so.
			}
			return m.runCommand(c.Name, ""), true
		}
		return nil, false
	}
	if opts := m.optMatches(); len(opts) > 0 {
		_, c, _ := m.cmdParse()
		// Claude Code-style quick pick: from the fresh selector, 1-9
		// choose that row outright. Once a filter is being typed, digits
		// join it instead (session titles carry dates).
		if k := msg.String(); len(k) == 1 && k[0] >= '1' && k[0] <= '9' && m.selectorFilter() == "" {
			if i := int(k[0] - '1'); i < len(opts) {
				return m.runCommand(c.Name, opts[i].Name), true
			}
		}
		switch msg.String() {
		case "up":
			m.cmdIdx = (m.cmdIdx - 1 + len(opts)) % len(opts)
			return nil, true
		case "down":
			m.cmdIdx = (m.cmdIdx + 1) % len(opts)
			return nil, true
		case "tab":
			o := opts[m.cmdIdx%len(opts)]
			m.resize(func() { m.input.SetValue("/" + c.Name + " " + o.Name) })
			return nil, true
		case "enter":
			return m.runCommand(c.Name, opts[m.cmdIdx%len(opts)].Name), true
		}
	}
	return nil, false
}

// entered turns a submitted draft into a command when its first word
// names one — the path for a fully typed "/profiles bravo". Anything else
// is an ordinary message.
func (m *Model) entered(text string) (tea.Cmd, bool) {
	if !strings.HasPrefix(text, "/") {
		return nil, false
	}
	name, args, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
	for _, c := range m.commands {
		if c.Name == strings.ToLower(name) {
			return m.runCommand(c.Name, strings.TrimSpace(args)), true
		}
	}
	return nil, false
}

// runCommand clears the composer and hands the command to the shell.
func (m *Model) runCommand(name, args string) tea.Cmd {
	m.cmdCloseMenu()
	m.draft = ""
	return func() tea.Msg { return CommandMsg{Name: name, Args: args} }
}

// viewCommandMenu renders the stage-1 command list above the composer:
// one row per match, the selected one banded, names in a fixed column
// and descriptions dim.
func (m *Model) viewCommandMenu(width int) string {
	matches := m.cmdMatches()
	if len(matches) == 0 {
		return ""
	}
	if m.cmdIdx >= len(matches) {
		m.cmdIdx = 0
	}
	// The cursor indexes matches; rows carry a blank between the session
	// group and the rest on the bare menu, so the two are mapped here.
	spacer := m.cmdSpacerAt(matches)
	type row struct{ name, desc string }
	var rows []row
	cursor := -1
	nameW := 12
	for i, c := range matches {
		if i == spacer {
			rows = append(rows, row{})
		}
		if i == m.cmdIdx {
			cursor = len(rows)
		}
		rows = append(rows, row{"/" + c.Name, c.Desc})
		nameW = max(nameW, lipgloss.Width("/"+c.Name)+2)
	}
	start := 0
	if cursor >= menuRows { // keep the cursor visible
		start = cursor - menuRows + 1
	}
	name := lipgloss.NewStyle().Width(nameW)
	var b strings.Builder
	for i := start; i < len(rows) && i-start < menuRows; i++ {
		switch {
		case rows[i].name == "":
			b.WriteString(theme.DimStyle.Width(width).Render(""))
		case i == cursor:
			b.WriteString(theme.SelectionStyle.Width(width).Render(" " + name.Render(rows[i].name) + rows[i].desc))
		default:
			b.WriteString(theme.DimStyle.Width(width).Render(" " + name.Render(rows[i].name) + rows[i].desc))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// MenuHelp is the menu's keys for the key sheet — display-only there,
// the real handling is cmdKey.
func (m *Model) MenuHelp() []key.Binding {
	return []key.Binding{m.keys.Commands, m.keys.MenuNext, m.keys.MenuFill, m.keys.MenuRun}
}
