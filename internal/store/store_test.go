// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package store

import (
	"testing"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/provider"
)

func TestTurnLifecycle(t *testing.T) {
	s := New(10)

	user := s.AppendTurn(provider.RoleUser, "hello", Complete)
	bot := s.AppendTurn(provider.RoleAssistant, "", InProgress)
	s.AppendToTurn(bot, "Hi")
	s.AppendToTurn(bot, " there")
	s.CompleteTurn(bot)

	turns := s.Turns()
	if len(turns) != 2 {
		t.Fatalf("Turns() = %d entries; want 2", len(turns))
	}
	if turns[0].ID != user || turns[0].Role != provider.RoleUser {
		t.Errorf("turn 0 = %+v; want the user turn", turns[0])
	}
	if turns[1].Content != "Hi there" || turns[1].Status != Complete {
		t.Errorf("turn 1 = %+v; want streamed content completed", turns[1])
	}
}

func TestRecordLifecycle(t *testing.T) {
	s := New(10)

	turn := s.AppendTurn(provider.RoleUser, "q", Complete)
	id := s.BeginRecord(capture.Event{Kind: capture.KindRequest, Status: 200})
	s.Link(turn, id)
	s.FinishBody(capture.Event{Kind: capture.KindBody, BodyBytes: 512})
	s.SetUsage(&provider.Usage{Prompt: 1, Completion: 2, Total: 3})
	s.FinishRecord()

	rec, ok := s.RecordByID(id)
	if !ok {
		t.Fatal("RecordByID() = false; want the record")
	}
	if rec.Status != Complete || rec.Body.BodyBytes != 512 || rec.Usage == nil || rec.Usage.Total != 3 {
		t.Errorf("record = %+v; want complete with body and usage", rec)
	}
	if got, _ := s.TurnByID(turn); got.RecordID != id {
		t.Errorf("turn.RecordID = %d; want %d", got.RecordID, id)
	}
}

func TestFinishRecordFailsHTTPErrors(t *testing.T) {
	s := New(10)
	s.BeginRecord(capture.Event{Kind: capture.KindRequest, Status: 403})
	s.FinishRecord()
	if rec, _ := s.LastRecord(); rec.Status != Failed {
		t.Errorf("status after 403 finish = %v; want Failed", rec.Status)
	}
}

func TestFailCurrentAndFailRecord(t *testing.T) {
	s := New(10)

	// Stream error on an open record.
	s.BeginRecord(capture.Event{Kind: capture.KindRequest, Status: 400})
	s.FailCurrent("400 content_filter: nope")
	rec, _ := s.LastRecord()
	if rec.Status != Failed || rec.Err == "" {
		t.Errorf("record = %+v; want failed with error text", rec)
	}

	// Transport error with no response at all.
	id := s.FailRecord("dial tcp: connection refused")
	rec, _ = s.RecordByID(id)
	if rec.Status != Failed || rec.Req.Method != "" || rec.Time.IsZero() {
		t.Errorf("transport record = %+v; want failed, empty request, timestamped", rec)
	}
}

func TestTrimKeepsIDsResolvable(t *testing.T) {
	s := New(3)
	var last int
	for range 5 {
		last = s.BeginRecord(capture.Event{Kind: capture.KindRequest, Status: 200})
		s.FinishRecord()
	}
	if len(s.Records()) != 3 {
		t.Fatalf("Records() = %d; want bounded to 3", len(s.Records()))
	}
	if _, ok := s.RecordByID(0); ok {
		t.Error("RecordByID(0) = ok; want trimmed away")
	}
	if rec, ok := s.RecordByID(last); !ok || rec.ID != last {
		t.Errorf("RecordByID(%d) = %+v,%v; want the newest record", last, rec, ok)
	}
}

func TestVersionBumpsOnMutation(t *testing.T) {
	s := New(10)
	v := s.Version()
	turn := s.AppendTurn(provider.RoleUser, "q", Complete)
	s.AppendToTurn(turn, "!")
	id := s.BeginRecord(capture.Event{Kind: capture.KindRequest})
	s.Link(turn, id)
	s.FinishRecord()
	if s.Version() <= v+3 {
		t.Errorf("Version() = %d after 5 mutations from %d; want bumped each time", s.Version(), v)
	}

	before := s.Version()
	s.Records()
	s.Turns()
	if s.Version() != before {
		t.Error("reads bumped Version(); want unchanged")
	}
}

func TestToolTurns(t *testing.T) {
	s := New(10)

	bot := s.AppendTurn(provider.RoleAssistant, "checking…", Complete)
	calls := []provider.ToolCall{{ID: "call_1", Name: "get_stations", Arguments: `{"station_code":"HWD"}`}}
	v := s.Version()
	s.SetToolCalls(bot, calls)
	if s.Version() == v {
		t.Error("SetToolCalls did not bump Version()")
	}

	res := s.AppendToolResult("call_1", "get_stations", `{"trains":[]}`)
	turns := s.Turns()
	if len(turns) != 2 {
		t.Fatalf("Turns() = %d entries; want 2", len(turns))
	}
	if got := turns[0].ToolCalls; len(got) != 1 || got[0].Name != "get_stations" {
		t.Errorf("assistant ToolCalls = %+v; want the recorded call", got)
	}
	tr := turns[1]
	if tr.ID != res || tr.Role != provider.RoleTool || tr.Status != Complete {
		t.Errorf("tool turn = %+v; want a complete RoleTool turn", tr)
	}
	if tr.ToolCallID != "call_1" || tr.ToolName != "get_stations" || tr.Content != `{"trains":[]}` {
		t.Errorf("tool turn = %+v; want call linkage, name, and result body", tr)
	}
}
