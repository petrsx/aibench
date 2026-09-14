// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

// Which record the panels show, from the keyboard. Clicking a record line
// (or a Tokens bar) is the mouse's way in — see mouse.go; these are the
// same move without leaving the keys, and the Inspector's ctrl+←/→ walks
// the very same store pin, so the two tabs never disagree about which
// record is "the one".

// stepRecord walks the pin by whole records. The transcript is re-rendered
// without following the bottom: the reader is looking at a block, and a
// jump to the newest line would take it away from them.
func (m *Model) stepRecord(by int) {
	if !m.state.StepPin(by) {
		return
	}
	m.renderMetrics()
	m.renderChat(false)
}

// showLatest releases the pin, so the panels follow the newest record
// again — the state a session starts in.
func (m *Model) showLatest() {
	if m.state.Pinned() < 0 {
		return
	}
	m.state.Pin(-1)
	m.renderMetrics()
	m.renderChat(false)
}
