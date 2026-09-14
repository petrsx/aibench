// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package prompt

import "charm.land/bubbles/v2/key"

// keyMap is the Prompt tab's own keys — the single source of truth for both
// matching (key.Matches in Update) and the help footer. Scroll and Edit are
// display-only: the view handles scroll keys itself, and the system prompt is
// edited in the file, not here.
type keyMap struct {
	NextField key.Binding
	Find      key.Binding
	// Copy takes the prompt as the file holds it — markdown, not the
	// rendered view: the file is the thing you meant to take.
	Copy   key.Binding
	Scroll key.Binding
	Edit   key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		NextField: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
		Find:      key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "find")),
		Copy:      key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "copy prompt")),
		Scroll:    key.NewBinding(key.WithHelp("↑/↓ pgup/pgdn", "scroll prompt")),
		Edit:      key.NewBinding(key.WithHelp("edit the prompt file", "params auto-save")),
	}
}

// ShortHelp is the tab's main control; the shell appends the global chords.
func (m *Model) ShortHelp() []key.Binding {
	return []key.Binding{m.keys.NextField, m.keys.Copy}
}

// FullHelp is the tab's expanded keymap. While the find overlay is open it
// documents the widget's match-walk keys instead.
func (m *Model) FullHelp() [][]key.Binding {
	if m.find.Active() {
		return [][]key.Binding{m.find.HelpBindings()}
	}
	return [][]key.Binding{
		{m.keys.NextField, m.keys.Find, m.keys.Copy, m.keys.Scroll},
		{m.keys.Edit},
	}
}
