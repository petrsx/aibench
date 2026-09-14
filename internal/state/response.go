// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package state

import (
	"context"
	"errors"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/store"
)

// ResponseMsg carries one chunk of the streamed reply into the message loop.
type ResponseMsg provider.Delta

// Progress is the streaming snapshot the Chat tab paints from; the tab pulls it
// each render rather than being fed.
type Progress struct {
	Streaming    bool
	StreamTurn   int // in-progress assistant turn id; -1 none
	PendingTurn  int // turn whose request is in flight; -1 idle
	PendingTools int // tool runs outstanding before the next request
	SendAt       time.Time
	SentChars    int
	Spinner      string // current spinner frame
}

// WaitResponse blocks on the next chunk of the streamed reply and wraps it as a
// ResponseMsg for the message loop.
func (s *State) WaitResponse() tea.Cmd {
	ch := s.stream
	return func() tea.Msg { return ResponseMsg(<-ch) }
}

// Receive folds one chunk of the streamed reply into the store: text onto the
// in-progress assistant turn, usage, errors, and — on the final chunk —
// finalizing the record and, when the reply requested bound tools, running
// them (the send then continues with a follow-up request once the results
// land). It returns the re-arm command, the tool-run batch, or nil once the
// send is complete. It is the read side of the request's stream.
func (s *State) Receive(d provider.Delta) tea.Cmd {
	if d.Content != "" {
		// The streamed reply is an in-progress turn from the first delta,
		// linked to the same record as the turn that triggered it (the
		// Inspector derives the assembled message from that link).
		if s.streamTurn < 0 {
			s.streamTurn = s.AppendTurn(provider.RoleAssistant, "", store.InProgress)
			if t, ok := s.TurnByID(s.pending); ok && t.RecordID >= 0 {
				s.Link(s.streamTurn, t.RecordID)
			}
		}
		s.AppendToTurn(s.streamTurn, d.Content)
	}
	if d.Usage != nil {
		s.SetUsage(d.Usage)
	}
	if d.Err != nil {
		// The record carries the condensed error for the diagnostics views; the
		// chat gets a bubble. An interrupt (esc) is the user's own doing — a
		// soft notice, not a red error.
		text, role := provider.Explain(d.Err), store.RoleError
		if errors.Is(d.Err, context.Canceled) {
			text, role = "interrupted", store.RoleNotice
		}
		slog.Warn("send failed", "err", text)
		s.FailCurrent(text)
		if r, ok := s.LastRecord(); !ok || r.Status != store.Failed {
			// The failing request's capture event hasn't landed yet (the
			// delta won the race); Capture fails the record when it does —
			// and links it to the turn, which ending the send is about to
			// forget.
			s.pendingFail, s.pendingLink = text, s.pending
		}
		s.AppendTurn(role, text, store.Complete)
	}

	if d.Done {
		if d.ResponseID != "" {
			// The response was stored server-side (store: true); the
			// next request — the tool round below included — chains on it.
			s.lastRespID = d.ResponseID
		}
		if s.streamTurn >= 0 {
			s.CompleteTurn(s.streamTurn)
		}
		if d.Final != "" {
			s.SetFinal(d.Final) // the assembled body replaces the raw stream
		}
		s.FinishRecord()

		// Tool calls ride the Done chunk. Bound calls run and keep the send
		// alive — ToolResult dispatches the follow-up request once they all
		// land; unbound calls are inspect-only and end the send here.
		if d.Err == nil {
			if cmd := s.runTools(d.ToolCalls); cmd != nil {
				return cmd
			}
		}

		s.finishSend()
		return nil
	}

	return s.WaitResponse()
}

// finishSend closes the send: the streaming flag, the send-spanning context,
// and — recorded on the user turn that started it — the wall-clock Total
// across every round (incl. any 429 backoff/retries), which the record line
// surfaces next to the last request's own latency.
func (s *State) finishSend() {
	s.streaming = false
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.streamTurn = -1
	s.pendingTools = 0
	if s.sendTurn >= 0 {
		s.SetTotal(s.sendTurn, time.Since(s.sentAt))
	}
	s.pending = -1
	s.sendTurn = -1
}

// Progress is the current streaming snapshot.
func (s *State) Progress() Progress {
	return Progress{
		Streaming:    s.streaming,
		StreamTurn:   s.streamTurn,
		PendingTurn:  s.pending,
		PendingTools: s.pendingTools,
		SendAt:       s.sentAt,
		SentChars:    s.sentChars,
		Spinner:      s.spin.View(),
	}
}
