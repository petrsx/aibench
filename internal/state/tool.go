// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package state

import (
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/store"
)

// ToolResultMsg carries one finished tool run into the message loop.
type ToolResultMsg struct {
	Call    provider.ToolCall
	Content string
	Err     error
}

// runTools folds a reply's tool calls into the store (on the assistant turn,
// for display and replay) and starts the bound ones. It returns the tool-run
// batch, or nil when nothing is bound — the caller then ends the send,
// leaving unbound calls visible for inspection.
func (s *State) runTools(calls []provider.ToolCall) tea.Cmd {
	if len(calls) == 0 {
		return nil
	}

	// A tool-only reply has no text delta, so no assistant turn is open yet.
	if s.streamTurn < 0 {
		s.streamTurn = s.AppendTurn(provider.RoleAssistant, "", store.Complete)
		if t, ok := s.TurnByID(s.pending); ok && t.RecordID >= 0 {
			s.Link(s.streamTurn, t.RecordID)
		}
	}
	s.SetToolCalls(s.streamTurn, calls)

	var bound []provider.ToolCall
	for _, c := range calls {
		if s.req.Bound(c.Name) {
			bound = append(bound, c)
		}
	}
	if len(bound) == 0 {
		return nil
	}
	if s.toolRounds >= maxToolRounds {
		s.AppendTurn(store.RoleNotice,
			fmt.Sprintf("tool loop stopped after %d rounds", maxToolRounds), store.Complete)
		return nil
	}
	s.toolRounds++
	s.pendingTools = len(bound)
	s.streamTurn = -1

	cmds := make([]tea.Cmd, len(bound))
	for i, c := range bound {
		cmds[i] = s.RunTool(c)
	}
	return tea.Batch(cmds...)
}

// RunTool executes one bound tool call inside a tea.Cmd — the HTTP request
// runs in the command's goroutine, never in the message loop — and wraps
// the result as a ToolResultMsg.
func (s *State) RunTool(call provider.ToolCall) tea.Cmd {
	req, ctx := s.req, s.ctx
	return func() tea.Msg {
		content, err := req.Run(ctx, call)
		return ToolResultMsg{Call: call, Content: content, Err: err}
	}
}

// ToolResult folds one finished tool run into the store as a tool-result
// turn (errors become the result text, so the model can react — except an
// interrupt, which ends the send with a notice) and, once every run of the
// round has landed, dispatches the follow-up request.
func (s *State) ToolResult(msg ToolResultMsg) tea.Cmd {
	if errors.Is(msg.Err, context.Canceled) {
		s.AppendTurn(store.RoleNotice, "interrupted", store.Complete)
		s.finishSend()
		return nil
	}

	content := msg.Content
	if msg.Err != nil {
		content = "error: " + msg.Err.Error()
	}
	// The follow-up request hangs under the tool-result turn: its record
	// line (and the pre-TTFB placeholder) render there, so every round of
	// the loop stays separately inspectable.
	s.pending = s.AppendToolResult(msg.Call.ID, msg.Call.Name, content)

	s.pendingTools--
	if s.pendingTools > 0 {
		return nil // more runs of this round still in flight
	}
	return s.dispatch()
}
