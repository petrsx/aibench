// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package inspector

import "charm.land/bubbles/v2/key"

// keyMap is the Inspector tab's own keys — the single source of truth for both
// matching (key.Matches in Update) and the help footer. The match-walk keys
// belong to the shared find widget, not here. Scroll and Drag are display-only:
// the viewport handles scroll keys and mouse drag itself, shown in help but
// never matched here.
type keyMap struct {
	Toggle key.Binding
	Search key.Binding // ctrl+f: opens the find overlay
	// Format flips the body between the wire form and its decoding: ctrl+f
	// is taken by find, so raw/rendered gets the r it is named for.
	Format key.Binding
	// Prev/Next walk the session's records without leaving the keyboard or
	// hunting for a row to click; Latest drops back to following the
	// newest. The Chat tab offers the same three over the same pin.
	// (Walking onto the newest releases the pin by itself — see
	// store.StepPin — so Latest is the shortcut, not the only way back.)
	Prev   key.Binding
	Next   key.Binding
	Latest key.Binding
	// Copy takes the body the pane is showing — the whole thing, as
	// received. Selecting part of it with the mouse still works; this is
	// for the common case of wanting all of it.
	Copy   key.Binding
	Scroll key.Binding
	Drag   key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Toggle: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "request/response")),
		Search: key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "find")),
		Format: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "raw/render")),
		// Shift+arrows, not ctrl: macOS takes the ctrl+arrow family for
		// Mission Control and Spaces. Vertical, matching the Chat tab and
		// the way a list is walked — plain ↑/↓ scrolls the body, exactly
		// as they do in crush.
		// One help entry for the pair, as the Chat tab does it: the walk
		// is one motion, and a line naming only one direction reads as a
		// tab you can move through in only one direction.
		Prev:   key.NewBinding(key.WithKeys("shift+up"), key.WithHelp("shift+↑/↓", "record")),
		Next:   key.NewBinding(key.WithKeys("shift+down"), key.WithHelp("shift+↑/↓", "record")),
		Latest: key.NewBinding(key.WithKeys("ctrl+end", "shift+end"), key.WithHelp("ctrl+end", "latest")),
		Copy:   key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "copy body")),
		Scroll: key.NewBinding(key.WithHelp("↑/↓ pgup/pgdn", "scroll")),
		Drag:   key.NewBinding(key.WithHelp("select text", "copy")),
	}
}

// ShortHelp is the tab's main control; the shell appends the global chords.
func (m *Model) ShortHelp() []key.Binding {
	return []key.Binding{m.keys.Toggle, m.keys.Format, m.keys.Copy, m.keys.Prev}
}

// FullHelp is contextual: while the find overlay is open it documents the
// widget's match-walk keys; otherwise the sub-view toggle, scroll, find, and
// drag-copy.
func (m *Model) FullHelp() [][]key.Binding {
	if m.find.Active() {
		return [][]key.Binding{m.find.HelpBindings()}
	}
	return [][]key.Binding{
		{m.keys.Toggle, m.keys.Scroll},
		{m.keys.Search, m.keys.Copy, m.keys.Drag},
		{m.keys.Format, m.keys.Prev},
		// Next carries the same help text as Prev (one motion, one entry),
		// so listing both would print the line twice.
		{m.keys.Latest},
	}
}
