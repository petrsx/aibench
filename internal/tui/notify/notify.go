// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package notify is the transient on-screen notice shared by the tabs: a
// pre-styled line (e.g. "copied to clipboard") that shows for a moment and then
// clears itself. Each view holds one Notification and renders it in its own
// spot; the timing and self-clear live here so the views don't each re-implement
// them.
package notify

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// duration is how long a notice stays on screen. Long enough to read a
// short line without hunting for it: at a second and a half a notice was
// gone before the eye had left whatever prompted it. Six was tried and
// outstayed its welcome — a confirmation you have already read is just
// something still on the screen.
const duration = 4 * time.Second

// ExpiredMsg repaints a view once its notice lapses, so it disappears without
// waiting for the next unrelated event.
type ExpiredMsg struct{}

// Notification is a transient notice with a self-clear timer. The zero value is
// ready to use — no notice showing.
type Notification struct {
	text string
	at   time.Time
}

// Show sets the notice to the given (pre-styled) text and returns the command
// that repaints once it lapses.
func (n *Notification) Show(text string) tea.Cmd {
	n.text, n.at = text, time.Now()
	return tea.Tick(duration, func(time.Time) tea.Msg { return ExpiredMsg{} })
}

// Copy puts text on the clipboard and says so, which is the whole of what
// a copy is on every surface that has one: the panel's icon, a drag over
// the transcript, the Inspector's dump. OSC 52 means the terminal owns the
// write, so it works over SSH; a copy that said nothing would look like a
// click that missed. Empty text is not a copy — the notice would lie.
//
// notice is pre-styled by the caller, like Show's text.
func (n *Notification) Copy(text, notice string) tea.Cmd {
	if text == "" {
		return nil
	}
	return tea.Batch(tea.SetClipboard(text), n.Show(notice))
}

// Text is the notice currently showing, or "" when none is set or it has
// lapsed.
func (n *Notification) Text() string {
	if n.text == "" || time.Since(n.at) > duration {
		return ""
	}
	return n.text
}
