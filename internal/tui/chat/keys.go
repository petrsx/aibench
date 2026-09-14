// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

import "charm.land/bubbles/v2/key"

// keyMap is the Chat tab's own keys — the single source of truth for both
// matching (key.Matches in Update) and the help footer (ShortHelp/FullHelp).
// Drag and Click are display-only: mouse gestures the transcript handles, shown
// in help but never matched here.
type keyMap struct {
	Send key.Binding
	// Queue is what enter does while a reply streams: the message waits
	// its turn instead of being refused.
	Queue key.Binding
	// Diag hides the record lines under the turns, leaving the
	// conversation. The tab is a bench most of the time and a chat some of
	// the time; the panels beside it are unaffected.
	Diag key.Binding
	// Prev/Next/Latest move which record the panels show, without hunting
	// for a row to click — the Inspector's walk over the same pin.
	Prev     key.Binding
	Next     key.Binding
	Latest   key.Binding
	Commands key.Binding
	// The command menu's own keys, display-only: while it is open the
	// help line documents cycling, completing and running instead.
	MenuNext key.Binding
	MenuFill key.Binding
	MenuRun  key.Binding
	Newline  key.Binding
	HistPrev key.Binding
	HistNext key.Binding
	Find     key.Binding
	// Copy takes the whole transcript. Selecting part of it with the
	// mouse still works; this is for wanting all of it.
	Copy   key.Binding
	Scroll key.Binding
	Drag   key.Binding
	Click  key.Binding
}

// newKeyMap builds the Chat keys. The Newline binding carries both chords
// (shift+enter where the terminal grants keyboard enhancements, alt+enter
// everywhere); its help label is refined once we learn which the terminal
// supports — see SetKittyKeys.
func newKeyMap() keyMap {
	return keyMap{
		Send:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		Queue: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "queue message")),
		Diag:  key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "diagnostics")),
		// Shift+arrows, not ctrl: macOS binds the whole ctrl+arrow family
		// to Mission Control and Spaces, so those chords never reach a
		// terminal app at all. Vertical, because the records are a
		// vertical list and ↑/↓ is how a list is walked everywhere else
		// (crush moves item-to-item the same way, and keeps ←/→ for
		// scrolling sideways). Plain ↑/↓ belongs to the composer's
		// history, so the pair is free.
		// One help entry for both, the way history does it: two lines for
		// one motion reads as two features.
		Prev: key.NewBinding(key.WithKeys("shift+up"), key.WithHelp("shift+↑/↓", "record")),
		Next: key.NewBinding(key.WithKeys("shift+down"), key.WithHelp("shift+↑/↓", "record")),
		// The end of the list is where following resumes — ctrl+end is
		// crush's chord for exactly that, and shift+end is the same reach
		// on a keyboard without a ctrl+end.
		Latest: key.NewBinding(key.WithKeys("ctrl+end", "shift+end"), key.WithHelp("ctrl+end", "latest")),
		// The slash-command menu is opened by typing, not by a chord: the
		// key is declared so the help line shows it, never matched.
		Commands: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "commands")),
		MenuNext: key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "cycle")),
		MenuFill: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "complete")),
		MenuRun:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "run")),
		Newline:  key.NewBinding(key.WithKeys("shift+enter", "alt+enter"), key.WithHelp("alt+enter", "newline")),
		HistPrev: key.NewBinding(key.WithKeys("up"), key.WithHelp("↑/↓", "history")),
		HistNext: key.NewBinding(key.WithKeys("down"), key.WithHelp("↑/↓", "history")),
		Find:     key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "find")),
		// ctrl+y, not ctrl+c: ctrl+c is how every terminal program is
		// stopped, and it quits this one too.
		Copy: key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "copy chat")),
		// pgup/pgdn scroll the transcript; plain scroll keys would collide with
		// typing in the focused input, so only these page keys are bound.
		Scroll: key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "scroll")),
		Drag:   key.NewBinding(key.WithHelp("select text", "copy")),
		Click:  key.NewBinding(key.WithHelp("click record line", "pin record")),
	}
}

// SetKittyKeys refines the newline help label: with keyboard enhancements the
// terminal delivers shift+enter as a distinct key, so that is the one to show;
// otherwise alt+enter. The binding's keys are unchanged (both still match).
func (m *Model) SetKittyKeys(kitty bool) {
	newline := "alt+enter"
	if kitty {
		newline = "shift+enter"
	}
	m.keys.Newline.SetHelp(newline, "newline")
}

// StreamingHelp is ShortHelp as it reads while a reply is arriving — the
// same controls, with enter saying what it now does. Exported for the
// shell's tests, which cannot start a real stream without an endpoint.
func (m *Model) StreamingHelp() []key.Binding {
	prog := m.prog
	m.prog.Streaming = true
	defer func() { m.prog = prog }()
	return m.ShortHelp()
}

// ShortHelp is the tab's main controls; the shell appends the global chords.
// While the command menu is open it documents the menu instead — those keys
// mean something else for as long as it is.
func (m *Model) ShortHelp() []key.Binding {
	if m.cmdMenuOpen() {
		// What the key does first, then how to move among the choices:
		// enter is why the menu is open, and cycling is how you get to
		// the one you meant.
		return []key.Binding{m.keys.MenuRun, m.keys.MenuNext, m.keys.MenuFill}
	}
	// Diag and Latest are deliberately absent: the line is the controls
	// you reach for while working, and it is also what the version and
	// the project link have to fit beside (credit.go). Both keep their
	// rows on the key sheet — and walking to the newest record releases
	// the pin by itself, so Latest is a shortcut, not the only way back.
	send := m.keys.Send
	if m.prog.Streaming {
		// Typing is not refused mid-stream; the message queues. Saying
		// "send" would promise the wrong thing.
		send = m.keys.Queue
	}
	return []key.Binding{send, m.keys.Commands, m.keys.Find, m.keys.Copy}
}

// FullHelp is the tab's expanded keymap. While the find overlay is open it
// documents the widget's match-walk keys instead.
func (m *Model) FullHelp() [][]key.Binding {
	if m.find.Active() {
		return [][]key.Binding{m.find.HelpBindings()}
	}
	return [][]key.Binding{
		{m.keys.Send, m.keys.Commands, m.keys.Newline, m.keys.HistPrev, m.keys.Scroll},
		{m.keys.Find, m.keys.Copy, m.keys.Drag, m.keys.Click, m.keys.Diag},
		// Next carries the same help text as Prev (one motion, one entry),
		// so listing both would print the line twice.
		{m.keys.Prev, m.keys.Latest},
	}
}
