// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package state is the live layer over the store: it owns the in-flight request
// — assemble (Send) → receive the streamed reply (Receive) → capture the raw
// HTTP traffic (Capture) — plus the streaming Progress snapshot. Together the store and
// this state are the only things the tabs share; views read from it, and it
// never reaches back into a view. It is mutated only inside the Bubble Tea
// message loop, so it needs no locking.
package state

import (
	"context"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/petrsx/aibench/internal/bindings"
	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/store"
)

// maxToolRounds caps the tool loop per send, so a model that keeps
// requesting tools can't spin the bench forever.
const maxToolRounds = 10

// State is the live request over the store. It embeds *store.Store so views
// hold a single handle and read the session history straight through it.
type State struct {
	*store.Store

	client    provider.Client
	captureCh chan capture.Event

	// the in-flight send; pending/sendTurn/streamTurn are -1 when idle.
	pending    int // turn the in-flight request hangs under (user turn, or a tool-result turn on later rounds)
	sendTurn   int // user turn that started the send; carries the loop's Total
	streamTurn int // in-progress assistant turn id
	streaming  bool
	stream     <-chan provider.Delta
	ctx        context.Context // spans the whole send, tool rounds included
	cancel     context.CancelFunc
	sentAt     time.Time // when the send was dispatched
	sentChars  int       // prompt size shipped with the last request, for the wait status

	// the send's tool loop: stashed request inputs (dispatch re-sends them
	// each round) and the loop's progress.
	sys          string
	params       provider.Params
	req          bindings.Set
	pendingTools int // tool runs still outstanding this round
	toolRounds   int // follow-up requests dispatched, capped at maxToolRounds

	// reqRecord is the record of the request in flight, -1 until its
	// capture event lands. Deltas and the capture's request event race
	// through the message loop; whatever a delta says about the record
	// (usage, the assembled body, completion, failure) is applied only
	// once the record exists, and held in the pending* fields until then.
	reqRecord    int
	pendingUsage *provider.Usage
	pendingFinal string
	pendingDone  bool
	// pendingFail carries a send failure whose record hasn't materialized
	// yet — Capture applies the text once the request event lands.
	pendingFail string
	// pendingLink is the turn that failure left waiting for a record. The
	// same race costs the *link*, not just the status: ending the send
	// clears `pending`, so a record materializing afterwards has nothing to
	// attach to and the message loses the record line under it — while the
	// record itself is there, in Metrics and the Inspector, which is a
	// worse state than either. -1 when nothing is waiting.
	pendingLink int

	// lastRespID chains stored responses (responses api, `store: true`):
	// the id of the last stored response, sent as previous_response_id on
	// the next request. Cleared on profile switch and context reset — a
	// chain must not cross either boundary.
	lastRespID string

	spin spinner.Model
}

// New builds the live state over st, driving client and reading captured HTTP
// events from captureCh. The spinner is injected so state needn't import the
// tui theme; the shell owns its styling.
func New(st *store.Store, client provider.Client, captureCh chan capture.Event, spin spinner.Model) *State {
	return &State{
		Store:       st,
		client:      client,
		captureCh:   captureCh,
		pending:     -1,
		pendingLink: -1,
		reqRecord:   -1,
		sendTurn:    -1,
		streamTurn:  -1,
		spin:        spin,
	}
}

// Ready reports whether an endpoint client is configured. A profile whose
// credentials don't resolve leaves the app running without one: the TUI
// is still worth looking at (transcript, prompt, inspector, the profile
// picker), so sends are refused with a message rather than the whole app
// refusing to start.
func (s *State) Ready() bool { return s.client != nil }

// SetClient swaps the endpoint client, e.g. on a profile switch. Any
// stored-response chain belongs to the old endpoint and ends here.
func (s *State) SetClient(c provider.Client) {
	s.client = c
	s.lastRespID = ""
}

// SetSpinnerStyle re-colors the spinner; the shell calls it when the theme
// palette flips (the spinner was injected styled, see New).
func (s *State) SetSpinnerStyle(style lipgloss.Style) { s.spin.Style = style }

// Streaming reports whether a response is in flight.
func (s *State) Streaming() bool { return s.streaming }

// Cancel interrupts the in-flight request, if any.
func (s *State) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}

// Tick advances the streaming spinner and re-arms only while streaming.
func (s *State) Tick(msg spinner.TickMsg) tea.Cmd {
	if !s.streaming {
		return nil
	}
	var cmd tea.Cmd
	s.spin, cmd = s.spin.Update(msg)
	return cmd
}

// ResetLive clears the live layer after the store's content is replaced
// (a session load or a fresh session): no send in flight, and no stored-
// response chain — a loaded chain must not resurrect, the same boundary
// rule as the context divider and profile switches.
func (s *State) ResetLive() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.streaming = false
	s.pending = -1
	s.sendTurn = -1
	s.streamTurn = -1
	s.pendingTools = 0
	s.pendingFail = ""
	s.pendingLink = -1
	s.reqRecord = -1
	s.pendingUsage, s.pendingFinal, s.pendingDone = nil, "", false
	s.lastRespID = ""
}
