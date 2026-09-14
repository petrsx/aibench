// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// globalKeys are the chords the shell owns — deliberately few. The app's
// actions live in the slash-command registry (commands.go: /settings,
// /profiles, /profiles-edit, …), typed in the composer; the chords that remain are
// the ones a command cannot cover: leaving (quit/interrupt work even
// mid-stream, when the composer is busy) and moving between tabs. Quit
// and Interrupt are display-only, since their real handling is stateful
// (streaming interrupt, find-cancel) and can't reduce to a single
// binding. Each tab owns its own editing keys and exposes
// them via ShortHelp/FullHelp; the shell composes those with these.
type globalKeys struct {
	Tabs      key.Binding
	Quit      key.Binding
	Interrupt key.Binding
}

func newGlobalKeys(dev bool) globalKeys {
	// A developer build quits on a second esc too (dev.go); the help says
	// so only there, because a chord listed where it does nothing is worse
	// than one that isn't listed at all.
	quit := "ctrl+c / 'exit'"
	if dev {
		quit = "ctrl+c / esc esc / 'exit'"
	}
	return globalKeys{
		Tabs: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "cycle tabs")),
		Quit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp(quit, "quit")),
		// Bound, not merely described: a binding with no keys reports
		// itself disabled, and the help widget drops it — which is how
		// the streaming line came to say nothing but "cycle tabs". Esc is
		// still handled by the shell's own switch (the meaning depends on
		// what is open), so the keys here are for the help, not a second
		// route into the handler.
		Interrupt: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "interrupt")),
	}
}

// shortNormal is the global tail of the short help line, appended after the
// active tab's own main controls.
func (k globalKeys) shortNormal() []key.Binding {
	return []key.Binding{k.Tabs}
}

// shortStreaming is the global tail while a response streams: esc
// interrupts, and tab-switching still works. It is a tail, not a
// replacement — the tab's own controls keep working mid-stream (find,
// scroll, the record walk, the command menu), and a line that dropped
// them read as "the app is busy, hands off".
func (k globalKeys) shortStreaming() []key.Binding {
	return []key.Binding{k.Interrupt, k.Tabs}
}

// full is the global column of the key sheet (/help): the few real
// chords — every other action is a slash command, listed in its own
// group there.
func (k globalKeys) full() []key.Binding {
	return []key.Binding{k.Tabs, k.Quit}
}

// handleKey is the shell's own key handling: the few chords a slash command
// cannot cover, plus the escape semantics that depend on what is open. done
// reports that the key was the shell's — false hands it to the active tab,
// where every other key belongs (Update routes it there).
func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	// The settings screen owns the content region while it is open:
	// every key is its own (esc closes) — only quitting outranks it.
	if m.settings != nil && !m.quitChord(msg) {
		return m.updateSettings(msg), true
	}
	switch msg.String() {
	case "ctrl+c":
		m.state.Cancel()
		return tea.Quit, true
	case "alt+esc":
		// Two escapes in quick succession reach the app coalesced, so the
		// dev quit has to answer this spelling as well as two separate
		// presses. Outside a developer build it means nothing.
		if m.dev {
			m.state.Cancel()
			return tea.Quit, true
		}
	case "esc":
		return m.handleEsc(), true
	}
	// The one remaining action chord: everything else is a slash
	// command, typed in the composer (commands.go).
	if key.Matches(msg, m.keys.Tabs) {
		return m.setTab((m.activeTab + 1) % tabCount), true
	}
	return nil, false
}

// handleEsc is the escape ladder, innermost first: an open find overlay on
// the active tab claims esc (close it, keep the app running), then a
// streaming response is interrupted. With neither open esc does nothing,
// and leaving is ctrl+c or typing exit — except in a developer build,
// where a second esc within the second quits (dev.go).
func (m *Model) handleEsc() tea.Cmd {
	if m.tabs[m.activeTab].model.CancelFind() {
		return nil
	}
	if !m.state.Streaming() {
		return m.devEscQuit()
	}
	// Interrupting stops a running script with it. Anything queued behind
	// the interrupted send is dropped too: esc means stop, and silently
	// sending the backlog afterwards would be the opposite.
	m.stopScript()
	m.state.Cancel()
	if n := len(m.sendQueue); n > 0 {
		m.sendQueue = nil
		m.chat.SetQueued(nil)
		return m.notifyWarn(fmt.Sprintf("interrupted — %d queued message(s) dropped", n))
	}
	return nil
}

// devEscQuit arms the developer build's quit window, or closes it: a
// second esc inside the second leaves. The window is invisible by nature,
// so arming it puts a hint in the help line (view.go) and schedules the
// repaint that clears it — without which the app would quit out of a
// state nothing on screen announced.
func (m *Model) devEscQuit() tea.Cmd {
	if !m.dev {
		return nil
	}
	if time.Since(m.lastEsc) <= escWindow {
		m.state.Cancel()
		return tea.Quit
	}
	m.lastEsc = time.Now()
	return tea.Tick(escWindow, func(time.Time) tea.Msg { return escTimeoutMsg{} })
}

// escWindow is how long the second esc has to land.
const escWindow = time.Second

// quitChord reports the chords that outrank the settings screen while it
// is open. Only leaving does — everything else on that screen is its own.
func (m *Model) quitChord(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "ctrl+c":
		return true
	case "alt+esc": // two coalesced escapes; a developer build only
		return m.dev
	}
	return false
}
