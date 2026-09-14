// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

import (
	"strconv"
	"strings"
	"testing"

	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/store"
)

// benchTranscript builds a session the size a real testing afternoon reaches:
// n exchanges, each a question, a tool call, and a folded JSON payload.
func benchTranscript(n int) *Model {
	m := newChatModel()
	var items strings.Builder
	items.WriteByte('[')
	for i := range 200 {
		if i > 0 {
			items.WriteByte(',')
		}
		items.WriteString(`{"id":"` + strconv.Itoa(i) + `","name":"` + strings.Repeat("x", 60) + `"}`)
	}
	items.WriteByte(']')
	payload := items.String()

	for i := range n {
		addRecord(m.state, 200, nil)
		id := strconv.Itoa(i)
		call := m.state.AppendTurn(provider.RoleAssistant, "", store.Complete)
		m.state.SetToolCalls(call, []provider.ToolCall{
			{ID: "call_" + id, Name: "search_locations", Arguments: `{"text":"Paris","limit":10}`},
		})
		m.state.AppendToolResult("call_"+id, "search_locations", payload)
		m.state.AppendTurn(provider.RoleAssistant, "a reply of ordinary length that wraps across the pane", store.Complete)
	}
	return m
}

// BenchmarkRenderChat is the frame cost while a reply streams: the whole
// transcript is re-derived on every spinner tick, so this is what the app
// pays ten times a second.
func BenchmarkRenderChat(b *testing.B) {
	m := benchTranscript(30)
	b.ReportAllocs()
	for b.Loop() {
		m.renderChat(true)
	}
}
