// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/store"
)

// TestRuleStaysInItsExchange pins the rule's reach: three questions, each
// with a tool round, and only the one holding the shown record is marked.
// The rule is painted over a range, so an over-wide range would quietly
// mark neighbours — which is exactly what it must not do, since the mark
// is what says "this question".
func TestRuleStaysInItsExchange(t *testing.T) {
	m := newChatModel()
	var firsts []int
	for n := range 3 {
		addRecord(m.state, 200, nil)
		first, _ := m.state.LastRecord()
		firsts = append(firsts, first.ID)
		call := m.state.AppendTurn(provider.RoleAssistant, "", store.Complete)
		m.state.SetToolCalls(call, []provider.ToolCall{{ID: fmt.Sprintf("c%d", n), Name: "get_weather", Arguments: "{}"}})
		m.state.Link(call, first.ID)
		round := m.state.BeginRecord(capture.Event{Kind: capture.KindRequest, Status: 200, Method: "POST", Path: "/p"})
		m.state.FinishBody(capture.Event{Kind: capture.KindBody})
		m.state.FinishRecord()
		tool := m.state.AppendToolResult(fmt.Sprintf("c%d", n), "get_weather", "17C")
		m.state.Link(tool, round)
		m.state.Link(m.state.AppendTurn(provider.RoleAssistant, "answer", store.Complete), round)
	}
	m.state.Pin(firsts[1]) // the middle question
	m.renderChat(false)

	inExchange := map[int]bool{}
	for _, r := range m.exchangeOf(firsts[1]) {
		inExchange[r.ID] = true
	}
	for i := range m.chatLines {
		plain := ansi.Strip(m.chatLines[i])
		ruled := strings.HasPrefix(plain, gutterRule) || strings.HasPrefix(plain, exchangeRule)
		owner := m.lineRec[i]
		if ruled && owner >= 0 && !inExchange[owner] {
			t.Errorf("row %d is ruled but belongs to record %d, outside the shown exchange: %q",
				i, owner, strings.TrimSpace(plain))
		}
	}
}

// TestRoundBlockIsItsOwnRows pins where a round's block starts and stops:
// the ● line that heads it, its request, and what that request brought
// back — and nothing else. The reply asking for a call is produced by the
// *previous* request, so owning the call line that way ran the rule two
// rows past where the block reads as ending, and made a click on it select
// the round before.
func TestRoundBlockIsItsOwnRows(t *testing.T) {
	m := newChatModel()
	addRecord(m.state, 200, nil)
	first, _ := m.state.LastRecord()
	call := m.state.AppendTurn(provider.RoleAssistant, "", store.Complete)
	m.state.SetToolCalls(call, []provider.ToolCall{{ID: "c0", Name: "tool", Arguments: "{}"}})
	m.state.Link(call, first.ID)
	round := m.state.BeginRecord(capture.Event{Kind: capture.KindRequest, Status: 200, Method: "POST", Path: "/p"})
	m.state.FinishBody(capture.Event{Kind: capture.KindBody})
	m.state.FinishRecord()
	tool := m.state.AppendToolResult("c0", "tool", strings.Repeat("x", 300))
	m.state.Link(tool, round)
	m.state.Link(m.state.AppendTurn(provider.RoleAssistant, "the answer", store.Complete), round)

	m.state.Pin(round)
	m.renderChat(false)

	var heavy []string
	for _, l := range m.chatLines {
		if plain := ansi.Strip(l); strings.HasPrefix(plain, gutterRule) {
			heavy = append(heavy, strings.TrimSpace(plain[len(gutterRule):]))
		}
	}
	// The call line, the request it made, the result it carried, and the
	// answer that request produced — one run, no separator hanging off it.
	if len(heavy) == 0 || !strings.HasPrefix(heavy[0], "● tool(") {
		t.Fatalf("the round's block does not start at its call line: %q", heavy)
	}
	if last := heavy[len(heavy)-1]; last == "" {
		t.Errorf("the block ends on a separator: %q", heavy)
	}
	// Clicking the call line selects the round it heads.
	for i, l := range m.chatLines {
		if strings.Contains(ansi.Strip(l), "● tool(") {
			if got := m.recAt(i); got != round {
				t.Errorf("a click on the call line selects record %d; want the round it heads (%d)", got, round)
			}
			break
		}
	}
	// And the question's own block is just the question and its request.
	m.state.Pin(first.ID)
	m.renderChat(false)
	heavy = heavy[:0]
	for _, l := range m.chatLines {
		if plain := ansi.Strip(l); strings.HasPrefix(plain, gutterRule) {
			heavy = append(heavy, strings.TrimSpace(plain[len(gutterRule):]))
		}
	}
	if len(heavy) != 2 {
		t.Errorf("the first request's block is %d rows (%q); want its question and its request", len(heavy), heavy)
	}
}
