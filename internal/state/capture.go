// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package state

import (
	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/capture"
)

// CaptureMsg carries a captured HTTP event into the message loop.
type CaptureMsg capture.Event

// WaitCapture blocks on the next captured HTTP event and wraps it as a
// CaptureMsg for the message loop.
func (s *State) WaitCapture() tea.Cmd {
	return func() tea.Msg { return CaptureMsg(<-s.captureCh) }
}

// Capture folds a captured HTTP event (request begin, body, error) into the
// store as a Record. It is the write side of the capture; the Chat tab's
// record lines and the Inspector read those records back.
func (s *State) Capture(e capture.Event) {
	switch e.Kind {
	case capture.KindRequest:
		id := s.BeginRecord(e)
		switch {
		case s.pending >= 0:
			s.Link(s.pending, id)
		case s.pendingLink >= 0:
			// The send already ended (see pendingLink): link the turn it
			// left behind, so the message keeps its record line.
			s.Link(s.pendingLink, id)
			s.pendingLink = -1
		}
		if s.streamTurn >= 0 { // first delta beat TTFB: link the reply too
			s.Link(s.streamTurn, id)
		}
		if s.pendingFail != "" {
			// The send already failed but its record only now materialized
			// (the error delta won the race through the message loop).
			s.FailCurrent(s.pendingFail)
			s.pendingFail = ""
		}
	case capture.KindBody:
		s.FinishBody(e)
	case capture.KindError:
		s.FailRecord(e.Text)
	}
}
