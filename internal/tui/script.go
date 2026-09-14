// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

// Script playback: one starter script's prompts sent in order, each
// after the previous reply completes — the TUI twin of `send --script`.
// Started from the /starters selector; a failed send or an esc
// interrupt stops the run.

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/store"
)

// startScript begins playback of one script (an index into m.scripts).
func (m *Model) startScript(set int) tea.Cmd {
	if set < 0 || set >= len(m.scripts) || len(m.scripts[set].Prompts) == 0 || m.state.Streaming() {
		return nil
	}
	m.scriptSet, m.scriptIdx = set, 0
	m.scriptRunning = true
	s := m.scripts[set]
	return tea.Batch(
		m.notifySay(fmt.Sprintf("running %s · %d prompts", s.Name, len(s.Prompts))),
		m.send(s.Prompts[0].Text),
	)
}

// scriptNext advances playback after a completed send: the next starter
// goes out, unless the reply errored or the script is done.
func (m *Model) scriptNext() tea.Cmd {
	if !m.scriptRunning {
		return nil
	}
	if turns := m.state.Turns(); len(turns) > 0 && turns[len(turns)-1].Role == store.RoleError {
		m.scriptRunning = false
		return m.notifyError("script stopped on error")
	}
	m.scriptIdx++
	script := m.scripts[m.scriptSet]
	if m.scriptIdx >= len(script.Prompts) {
		m.scriptRunning = false
		return m.notifySay(script.Name + " done")
	}
	return m.send(script.Prompts[m.scriptIdx].Text)
}

// stopScript aborts playback (esc interrupts, profile switches).
func (m *Model) stopScript() {
	m.scriptRunning = false
}
