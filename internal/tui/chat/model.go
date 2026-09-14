// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package chat is the Chat tab: a Claude Code-style transcript with per-turn
// record lines, a Tokens graph, and a Metrics panel.
// It is an independent tea.Model over the shared session store. It owns the
// input and every visual pane but not the request lifecycle: on Enter it emits
// a SendMsg for the shell to orchestrate, and the shell feeds it a streaming
// Progress snapshot to render (spinner, pending/among turns).
package chat

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/preset"
	"github.com/petrsx/aibench/internal/pricing"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/state"
	"github.com/petrsx/aibench/internal/tui/find"
	"github.com/petrsx/aibench/internal/tui/notify"
	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/selection"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// SendMsg is the intent a completed Enter emits: the shell reads the Prompt
// tab, builds the message list, and drives the request.
type SendMsg struct{ Text string }

// QuitRequestMsg is the typed-"exit" intent — routed through the shell so
// the session flushes before the program quits.
type QuitRequestMsg struct{}

// Model is the Chat tab.
type Model struct {
	state   *state.State // the live layer; embeds the session store
	spec    provider.Spec
	cfg     config.Config
	pricing pricing.Table
	// header is the tab header's content — the shell's profile strip,
	// handed down on every profile or prompt switch.
	header string
	keys   keyMap // this tab's keys — matching and help both read them

	input    textarea.Model
	chatView viewport.Model

	// transcript search (ctrl+f): the shared find widget shown as a top-right
	// overlay, plus the overlay's screen origin recorded at layout time for
	// click/cursor math.
	find find.Pane

	// derived caches the per-turn JSON work the transcript needs (memo.go);
	// derivedTop is the highest turn id it was built over, which is how a
	// cleared session is noticed.
	derived      map[int]*derived
	derivedTop   int
	findX, findY int

	prog        state.Progress // streaming snapshot, pulled from state each render
	renderedVer int            // store version the caches were last derived at; -1 forces

	// input history: the profile's starter prompts (a seed segment that
	// stays browsable all session) followed by previously sent messages,
	// walked with up/down. histIdx indexes the combined list.
	starters []preset.Starter
	sent     []string
	histIdx  int
	draft    string

	// slash commands: the menu offered while the draft is a command in
	// the making (commands.go). The shell registers the list; cmdIdx is
	// the cursor, optCmd/optList cache one command's options while the
	// draft stays on it.
	commands []Command
	cmdIdx   int
	optCmd   string
	optList  []Option

	// caches for mouse selection: rendered content lines per pane.
	chatLines    []string
	metricsLines []string
	lineRec      []int     // record id per chatLines row (-1 none)
	lineResult   []int     // result turn id per ⎿ header row (-1 none)
	resultHits   []hitSpan // where those rows' controls are, in cells

	// opened result payloads (result turn id → true). The calls themselves
	// and the requests they cost always show — only a payload folds, so a
	// long body doesn't drown the conversation.
	resultOpen map[int]bool

	// Tokens graph click geometry, cached by viewTokens at render time.
	tokenBarRecs []int
	tokenBarX0   int

	// selection over the chat / metrics panes.
	sel selection.State

	note notify.Notification // transient corner notice (e.g. "copied to clipboard")

	// geometry: top is the screen row the content region begins at; width is
	// the full terminal width.
	top    int
	width  int
	height int // content region height (excludes header + help chrome)

	// queued mirrors the shell's send queue — messages typed while a send
	// was in flight — so the transcript can show them as pending instead
	// of hiding them behind a count in a notice.
	queued []string

	// tipIdx is which hint the header is showing; the shell advances it
	// on its timer (tips.go).
	tipIdx int

	// hideDiag drops the record lines under each turn, leaving the
	// conversation to be read as the thing the session produced rather
	// than as the requests that produced it. The panels beside it stay:
	// they are a column of their own, and hiding them would move the
	// text you are reading — the one thing a reading mode must not do.
	hideDiag bool

	// stickBottom keeps the transcript pinned to the newest line. The user
	// owns it: scrolling away clears it, returning to the bottom re-arms
	// it, and sending re-arms it too. Without it, the every-frame render
	// during streaming would fight any attempt to scroll back.
	stickBottom bool
}

// New builds the Chat tab. The api spec, endpoint cfg, and pricing table
// drive the record lines and Metrics panel, and header is the tab header's
// content; the shell refreshes them on a profile switch via SetProfile.
func New(st *state.State, spec provider.Spec, cfg config.Config, prices pricing.Table, header string) *Model {
	ta := newTextarea("Ask something… (enter to send, / for commands, ↑/↓ for history)")
	ta.Focus()
	m := &Model{
		state:       st,
		spec:        spec,
		cfg:         cfg,
		pricing:     prices,
		header:      header,
		keys:        newKeyMap(),
		find:        find.NewPane(),
		derived:     map[int]*derived{},
		derivedTop:  -1,
		input:       ta,
		prog:        state.Progress{StreamTurn: -1, PendingTurn: -1},
		stickBottom: true,
		renderedVer: -1,
		resultOpen:  map[int]bool{},
	}
	m.ApplyTheme() // first transcript render, in the active palette
	return m
}

// Init has nothing to arm: cursor blink is the terminal's job in v2 (the
// shell surfaces this tab's Cursor() on its tea.View).
func (m *Model) Init() tea.Cmd { return nil }

// Cursor is the real terminal cursor inside the input textarea, offset to
// screen coordinates (the input box sits under the chat pane, inside a
// border).
func (m *Model) Cursor() *tea.Cursor {
	// The find overlay's query input owns the cursor while search is open.
	if m.find.Active() {
		return m.find.Cursor(m.findX, m.findY)
	}
	// The selector frame replaces the input — no text cursor while a
	// pick is open.
	if m.selecting() {
		return nil
	}
	c := m.input.Cursor()
	if c == nil {
		return nil
	}
	c.X++                                        // input pane border
	c.Y += m.chatTop() + m.chatView.Height() + 1 // transcript + the input box's border
	return c
}

// SetQueued mirrors the shell's send queue into the transcript. Called
// whenever the queue changes: a message joins it, or one is dispatched.
func (m *Model) SetQueued(q []string) {
	m.queued = q
	m.renderChat(m.stickBottom)
}

// SetProfile re-points the tab at a new endpoint (a config profile or
// prompt-set switch), header included.
func (m *Model) SetProfile(spec provider.Spec, cfg config.Config, header string) {
	m.spec, m.cfg, m.header = spec, cfg, header
	m.refresh()
}

// Notify shows a pre-styled notice in this tab's own spot until it
// lapses. The shell uses it for app-level messages, handing them to
// whichever tab is in front: the placement is the tab's business, and
// each one already knows where a notice reads best in its layout.
func (m *Model) Notify(text string) tea.Cmd { return m.note.Show(text) }

// SetPricing supplies the model-id → rates table for the cost/context rows.
func (m *Model) SetPricing(t pricing.Table) {
	m.pricing = t
	m.renderMetrics()
}

// Focus/Blur move focus to/from the input (the shell drives this on tab and
// terminal-focus changes).
func (m *Model) Focus() tea.Cmd {
	return m.input.Focus()
}

func (m *Model) Blur() { m.input.Blur() }

// Update handles the Chat tab's keys (enter sends, ↑/↓ walk history) and
// mouse, plus notice repaints.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case notify.ExpiredMsg:
		return nil
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case tea.KeyPressMsg:
		// While the find overlay is open it claims the keys (typing the query,
		// walking matches); the transcript and input see nothing.
		if m.find.Active() {
			return m.find.Route(msg, m)
		}
		// The command menu claims ↑/↓, tab and enter while it is open —
		// the composer's own history walk and send wait behind it.
		if cmd, handled := m.cmdKey(msg); handled {
			return cmd
		}
		switch {
		case msg.String() == "tab":
			// Swallowed so the textarea never gets a literal tab —
			// deliberately absent from the help: it is a non-feature.
			return nil
		case key.Matches(msg, m.keys.Diag):
			// The transcript re-wraps around the rows that leave, so a
			// selection held over the old lines no longer means anything.
			m.hideDiag = !m.hideDiag
			m.SetSize(m.top, m.width, m.height)
			return nil
		case key.Matches(msg, m.keys.Prev):
			m.stepRecord(-1)
			return nil
		case key.Matches(msg, m.keys.Next):
			m.stepRecord(1)
			return nil
		case key.Matches(msg, m.keys.Latest):
			m.showLatest()
			return nil
		case key.Matches(msg, m.keys.Copy):
			// The transcript as shown — the record lines included, since
			// what is on screen is what a reader means by "the chat".
			return m.note.Copy(render.CopyText(m.chatLines),
				theme.NoticeStyle.Render("chat copied to clipboard"))
		case key.Matches(msg, m.keys.Find):
			return m.openFind()
		case key.Matches(msg, m.keys.Scroll):
			// Page the transcript without disturbing the focused input.
			if msg.String() == "pgup" {
				m.chatView.PageUp()
			} else {
				m.chatView.PageDown()
			}
			m.stickBottom = m.chatView.AtBottom()
			return nil
		case key.Matches(msg, m.keys.Send):
			// Enter always submits, streaming or not: the shell queues it
			// behind the send in flight rather than dropping the keystroke.
			if m.input.Value() == "exit" {
				return func() tea.Msg { return QuitRequestMsg{} }
			}
			// A draft naming a command runs it instead of sending.
			if cmd, ok := m.entered(strings.TrimSpace(m.input.Value())); ok {
				return cmd
			}
			return m.send()
		case key.Matches(msg, m.keys.Newline):
			// Newline without sending: shift+enter where the terminal grants
			// keyboard enhancements, alt+enter everywhere.
			before := m.input.Height()
			m.input.InsertRune('\n')
			m.syncInputHeight(before)
			return nil
		case key.Matches(msg, m.keys.HistPrev):
			if m.histPrev() {
				return nil
			}
		case key.Matches(msg, m.keys.HistNext):
			if m.histNext() {
				return nil
			}
		}
	}
	// The input's dynamic height — and the command menu above it — move the
	// pane boundary: re-carve the column when an edit grows or shrinks it.
	before := m.composerHeight()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.syncInputHeight(before)
	return cmd
}

// composerHeight is the rows the bottom block takes: the textarea plus
// the command menu when it is open, or the selector replacing them both.
func (m *Model) composerHeight() int {
	if m.selecting() {
		return m.selectorHeight()
	}
	return m.input.Height() + m.cmdMenuHeight()
}

// syncInputHeight re-carves the column when the composer block's height moved
// from the given starting point.
func (m *Model) syncInputHeight(before int) {
	if m.composerHeight() != before {
		m.SetSize(m.top, m.width, m.height)
	}
}

// View pulls the streaming snapshot, re-derives the cached panes when the
// store version moved — or every frame while streaming, so the spinner
// animates and the reply grows — and hands composition to layout (layout.go).
// This pull replaces the shell's old per-mutation push.
func (m *Model) View() string {
	m.prog = m.state.Progress()
	if v := m.state.Version(); v != m.renderedVer || m.prog.Streaming {
		m.refresh()
		m.renderedVer = v
	}
	return m.layout()
}
