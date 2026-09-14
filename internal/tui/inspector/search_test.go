// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package inspector

import (
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/state"
	"github.com/petrsx/aibench/internal/store"
)

func TestSearchMatchesStyledDump(t *testing.T) {
	st := state.New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
	turn := st.AppendTurn(provider.RoleUser, "q", store.Complete)
	id := st.BeginRecord(capture.Event{Kind: capture.KindRequest, Status: 200, Method: "POST", Path: "/p"})
	st.Link(turn, id)
	st.FinishBody(capture.Event{
		Kind: capture.KindBody,
		Body: `{"model":"gpt","usage":{"total_tokens":42},"note":"MODEL"}`,
	})
	st.FinishRecord()

	m := New(st)
	m.SetSize(3, 120, 36)
	m.view = viewResponse
	m.Refresh()

	m.openFind()
	for _, r := range "model" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	// The dump is colorized JSON: matching runs over the ANSI-stripped text
	// and is case-insensitive, so the key and the "MODEL" value both hit.
	if got := m.find.Matches(); got != 2 {
		t.Fatalf("matches = %d; want 2 (case-insensitive over the styled dump)", got)
	}

	// esc: CancelFind consumes the key once and clears the state.
	if !m.CancelFind() {
		t.Fatal("CancelFind() with search open = false; want true (esc consumed)")
	}
	if m.CancelFind() {
		t.Fatal("CancelFind() with search closed = true; want false (esc falls through)")
	}
	if got := m.find.Matches(); got != 0 {
		t.Error("match count survived CancelFind")
	}
}
