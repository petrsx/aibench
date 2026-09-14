// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package state

import (
	"context"
	"log/slog"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/bindings"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/store"
)

// Send starts a send: it stashes the request inputs (system prompt, params,
// request extensions — the caller reads them from the Prompt tab, so state
// never imports a view), appends the user turn, and dispatches the first
// request. Tool rounds re-dispatch through the same path (dispatch), each
// producing its own record.
func (s *State) Send(sys string, params provider.Params, req bindings.Set, text string) tea.Cmd {
	// A new message follows the latest again — drop any pinned record.
	s.Pin(-1)
	s.sys, s.params, s.req = sys, params, req
	s.toolRounds = 0
	s.pendingTools = 0
	// A stale failure — or a turn still waiting on a record that never
	// arrived — must not poison this send's records.
	s.pendingFail, s.pendingLink = "", -1

	s.sendTurn = s.AppendTurn(provider.RoleUser, text, store.Complete)
	s.pending = s.sendTurn
	s.streaming = true

	// One context spans the whole send — every tool round's request and
	// tool run — so esc interrupts whichever is in flight. Total (set on
	// the user turn at the end) spans the same.
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx, s.cancel = ctx, cancel
	s.sentAt = time.Now()

	return s.dispatch()
}

// dispatch assembles the request from the stashed inputs and the store
// history and starts the client stream: the first request of a send, and
// each follow-up carrying tool results.
func (s *State) dispatch() tea.Cmd {
	s.streamTurn = -1

	// The system prompt is prepended per request, not stored in the visible
	// history; error/notice bubbles are display-only and never sent. Tool
	// calls and results replay through the provider's own wire shapes. A
	// reset divider cuts the window: only turns after the last one go up.
	turns := s.Turns()
	msgs := make([]provider.Message, 0, len(turns)+1)
	if sys := strings.TrimSpace(s.sys); sys != "" {
		msgs = append(msgs, provider.Message{Role: provider.RoleSystem, Content: sys})
	}
	for _, t := range turns {
		if t.Role == store.RoleError || t.Role == store.RoleNotice {
			continue
		}
		msgs = append(msgs, provider.Message{
			Role:       t.Role,
			Content:    t.Content,
			ToolCalls:  t.ToolCalls,
			ToolCallID: t.ToolCallID,
		})
	}

	s.sentChars = 0
	for _, msg := range msgs {
		s.sentChars += len(msg.Content)
	}
	slog.Debug("dispatch", "messages", len(msgs), "chars", s.sentChars, "toolRound", s.toolRounds)

	// A per-request copy: the stored-response chain id must not stick to the
	// stashed params. Clients not in stateful mode ignore it.
	params := s.params
	params.PreviousResponseID = s.lastRespID
	s.stream = s.client.Stream(s.ctx, msgs, params, s.req.Overlay)

	return tea.Batch(s.WaitResponse(), s.spin.Tick)
}
