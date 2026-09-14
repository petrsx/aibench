// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

// The conversation lifecycle, shell-side: /clear (and a profile switch)
// resets the store to empty. Nothing is persisted — sessions are
// deliberately not a feature: a conversation worth repeating belongs in
// the profile's starter prompts, replayed exactly, not in a snapshot
// restored approximately (a stateful responses chain cannot resurrect
// server-side anyway).

import (
	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/store"
)

// clearSession resets the store to empty — the engine behind /clear and
// a profile switch.
func (m *Model) clearSession() {
	m.state.Import(store.Session{})
	m.state.ResetLive()
}

// newSession is /clear: flush the conversation and say so.
func (m *Model) newSession() tea.Cmd {
	if m.state.Streaming() {
		return nil
	}
	m.clearSession()
	return m.notifySay("conversation cleared")
}
