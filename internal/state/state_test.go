// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package state

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/bindings"
	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/store"
)

func newTestState() *State {
	return New(store.New(500), nil, make(chan capture.Event, 1), spinner.New())
}

func TestRecordHistory(t *testing.T) {
	s := newTestState()

	// Two request/response cycles, correlated via the pending turn.
	for i, status := range []int{200, 403} {
		s.pending = s.AppendTurn(provider.RoleUser, "q", store.Complete)
		s.Capture(capture.Event{Kind: capture.KindRequest, Status: status, Method: "POST", Path: "/p"})
		s.Capture(capture.Event{Kind: capture.KindBody, BodyBytes: int64(100 + i)})
		s.Receive(provider.Delta{Usage: &provider.Usage{Prompt: 1 + i, Completion: 2, Total: 3 + i}})
	}

	recs := s.Records()
	if len(recs) != 2 {
		t.Fatalf("records = %d; want 2", len(recs))
	}
	for i, want := range []struct {
		id, status int
		body       int64
		prompt     int
	}{{0, 200, 100, 1}, {1, 403, 101, 2}} {
		rec := recs[i]
		if rec.ID != want.id || rec.Req.Status != want.status ||
			rec.Body.BodyBytes != want.body || rec.Usage == nil || rec.Usage.Prompt != want.prompt {
			t.Errorf("records[%d] = id %d status %d body %d usage %+v; want %+v",
				i, rec.ID, rec.Req.Status, rec.Body.BodyBytes, rec.Usage, want)
		}
	}
	turns := s.Turns()
	if turns[0].RecordID != 0 || turns[1].RecordID != 1 {
		t.Errorf("turn RecordIDs = %d,%d; want 0,1", turns[0].RecordID, turns[1].RecordID)
	}
}

func TestStreamedTurnLifecycle(t *testing.T) {
	s := newTestState()
	s.pending = s.AppendTurn(provider.RoleUser, "q", store.Complete)
	s.streaming = true

	s.Receive(provider.Delta{Content: "Hel"})
	s.Receive(provider.Delta{Content: "lo"})
	turns := s.Turns()
	if got := turns[len(turns)-1]; got.Content != "Hello" || got.Status != store.InProgress {
		t.Fatalf("streaming turn = %+v; want in-progress \"Hello\"", got)
	}

	s.Receive(provider.Delta{Done: true})
	turns = s.Turns()
	if got := turns[len(turns)-1]; got.Status != store.Complete {
		t.Errorf("turn after done = %+v; want complete", got)
	}
	if s.streaming || s.streamTurn != -1 {
		t.Errorf("streaming state = %v/%d; want reset", s.streaming, s.streamTurn)
	}
}

func TestInterruptIsNoticeNotError(t *testing.T) {
	s := newTestState()
	s.pending = s.AppendTurn(provider.RoleUser, "q", store.Complete)
	s.streaming = true
	s.Receive(provider.Delta{Err: context.Canceled})

	turns := s.Turns()
	last := turns[len(turns)-1]
	if last.Role != store.RoleNotice || last.Content != "interrupted" {
		t.Errorf("interrupt turn = %+v; want a notice saying interrupted", last)
	}
}

// fakeClient scripts one delta sequence per Stream call and records what
// each request carried.
type fakeClient struct {
	rounds   [][]provider.Delta
	messages [][]provider.Message
	params   []provider.Params
}

func (f *fakeClient) Stream(_ context.Context, history []provider.Message, p provider.Params, _ provider.Overlay) <-chan provider.Delta {
	f.messages = append(f.messages, history)
	f.params = append(f.params, p)
	round := f.rounds[0]
	f.rounds = f.rounds[1:]
	ch := make(chan provider.Delta, len(round)+1)
	for _, d := range round {
		ch <- d
	}
	close(ch)
	return ch
}

// drive pumps a send to completion, executing returned commands and routing
// their messages the way the shell's Update does (spinner ticks dropped).
func drive(t *testing.T, s *State, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 100 {
			t.Fatal("drive: send did not finish within 100 steps")
		}
		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		switch msg := next().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case ResponseMsg:
			queue = append(queue, s.Receive(provider.Delta(msg)))
		case ToolResultMsg:
			queue = append(queue, s.ToolResult(msg))
		}
	}
}

func TestToolLoopRunsBoundCallAndResends(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"trains":["21:05"]}`))
	}))
	defer srv.Close()

	call := provider.ToolCall{ID: "call_1", Name: "get_stations", Arguments: `{"station_code":"HWD"}`}
	client := &fakeClient{rounds: [][]provider.Delta{
		{{Done: true, ToolCalls: []provider.ToolCall{call}}},
		{{Content: "The 21:05 runs."}, {Done: true}},
	}}
	s := New(store.New(500), client, make(chan capture.Event, 8), spinner.New())
	req := bindings.Set{Bindings: map[string]bindings.Binding{
		"get_stations": {URL: srv.URL + "/stations/{station_code}"},
	}}

	drive(t, s, s.Send("sys", provider.Params{}, req, "weather in Paris?"))

	if len(client.messages) != 2 {
		t.Fatalf("Stream calls = %d; want 2 (initial + follow-up)", len(client.messages))
	}
	// The follow-up replays the assistant's tool call and carries the result.
	follow := client.messages[1]
	var sawCall, sawResult bool
	for _, m := range follow {
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) == 1 && m.ToolCalls[0].ID == "call_1" {
			sawCall = true
		}
		if m.Role == provider.RoleTool && m.ToolCallID == "call_1" && m.Content == `{"trains":["21:05"]}` {
			sawResult = true
		}
	}
	if !sawCall || !sawResult {
		t.Errorf("follow-up messages = %+v; want replayed tool call and its result", follow)
	}

	turns := s.Turns()
	last := turns[len(turns)-1]
	if last.Role != provider.RoleAssistant || last.Content != "The 21:05 runs." {
		t.Errorf("final turn = %+v; want the follow-up answer", last)
	}
	if s.Streaming() {
		t.Error("Streaming() = true after the loop finished; want false")
	}
	if turns[0].Total <= 0 {
		t.Error("user turn Total unset; want the loop's wall-clock recorded")
	}
}

func TestUnboundToolCallEndsSendForInspection(t *testing.T) {
	call := provider.ToolCall{ID: "call_9", Name: "unbound_tool", Arguments: `{}`}
	client := &fakeClient{rounds: [][]provider.Delta{
		{{Done: true, ToolCalls: []provider.ToolCall{call}}},
	}}
	s := New(store.New(500), client, make(chan capture.Event, 8), spinner.New())

	drive(t, s, s.Send("", provider.Params{}, bindings.Set{}, "q"))

	if len(client.messages) != 1 {
		t.Fatalf("Stream calls = %d; want 1 (no follow-up for unbound calls)", len(client.messages))
	}
	turns := s.Turns()
	last := turns[len(turns)-1]
	if last.Role != provider.RoleAssistant || len(last.ToolCalls) != 1 || last.ToolCalls[0].Name != "unbound_tool" {
		t.Errorf("last turn = %+v; want the tool call kept visible for inspection", last)
	}
	if s.Streaming() {
		t.Error("Streaming() = true; want the send finished")
	}
}

func TestToolLoopCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	// The model asks for the same tool forever.
	call := provider.ToolCall{ID: "c", Name: "loop", Arguments: `{}`}
	rounds := make([][]provider.Delta, maxToolRounds+1)
	for i := range rounds {
		rounds[i] = []provider.Delta{{Done: true, ToolCalls: []provider.ToolCall{call}}}
	}
	client := &fakeClient{rounds: rounds}
	s := New(store.New(500), client, make(chan capture.Event, 64), spinner.New())
	req := bindings.Set{Bindings: map[string]bindings.Binding{"loop": {URL: srv.URL}}}

	drive(t, s, s.Send("", provider.Params{}, req, "go"))

	if got := len(client.messages); got != maxToolRounds+1 {
		t.Errorf("Stream calls = %d; want %d (initial + capped rounds)", got, maxToolRounds+1)
	}
	turns := s.Turns()
	last := turns[len(turns)-1]
	if last.Role != store.RoleNotice {
		t.Errorf("last turn = %+v; want the loop-cap notice", last)
	}
	if s.Streaming() {
		t.Error("Streaming() = true; want the send finished")
	}
}

func TestToolRunErrorFeedsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	call := provider.ToolCall{ID: "c1", Name: "t", Arguments: `{}`}
	client := &fakeClient{rounds: [][]provider.Delta{
		{{Done: true, ToolCalls: []provider.ToolCall{call}}},
		{{Content: "the tool failed"}, {Done: true}},
	}}
	s := New(store.New(500), client, make(chan capture.Event, 8), spinner.New())
	req := bindings.Set{Bindings: map[string]bindings.Binding{"t": {URL: srv.URL}}}

	drive(t, s, s.Send("", provider.Params{}, req, "q"))

	if len(client.messages) != 2 {
		t.Fatalf("Stream calls = %d; want 2 (the error goes back to the model)", len(client.messages))
	}
	var toolMsg provider.Message
	for _, m := range client.messages[1] {
		if m.Role == provider.RoleTool {
			toolMsg = m
		}
	}
	if !strings.HasPrefix(toolMsg.Content, "error: ") || !strings.Contains(toolMsg.Content, "down") {
		t.Errorf("tool result = %q; want the run error as content", toolMsg.Content)
	}
}

func TestStoredResponseChain(t *testing.T) {
	client := &fakeClient{rounds: [][]provider.Delta{
		{{Done: true, ResponseID: "resp_1"}},
		{{Done: true, ResponseID: "resp_2"}},
		{{Done: true, ResponseID: "resp_3"}},
	}}
	s := New(store.New(500), client, make(chan capture.Event, 8), spinner.New())

	drive(t, s, s.Send("sys", provider.Params{}, bindings.Set{}, "first"))
	drive(t, s, s.Send("sys", provider.Params{}, bindings.Set{}, "second"))

	if got := client.params[0].PreviousResponseID; got != "" {
		t.Errorf("first request PreviousResponseID = %q; want empty", got)
	}
	if got := client.params[1].PreviousResponseID; got != "resp_1" {
		t.Errorf("second request PreviousResponseID = %q; want resp_1", got)
	}

	// A profile switch forgets the chain — it belongs to the old endpoint.
	s.SetClient(client)
	if s.lastRespID != "" {
		t.Errorf("lastRespID after SetClient = %q; want empty", s.lastRespID)
	}
}

// TestFailedFollowUpKeepsResponseBody replays the capture/response event
// interleaving of a tool round whose follow-up request is rejected (e.g. a
// gateway 403): the body event lands on the failed record, not lost.
func TestFailedFollowUpKeepsResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	call := provider.ToolCall{ID: "c1", Name: "t", Arguments: `{}`}
	client := &fakeClient{rounds: [][]provider.Delta{
		{{Done: true, ToolCalls: []provider.ToolCall{call}}},
		{{Err: errors.New("403 Forbidden"), Done: true}},
	}}
	s := New(store.New(500), client, make(chan capture.Event, 8), spinner.New())
	req := bindings.Set{Bindings: map[string]bindings.Binding{"t": {URL: srv.URL}}}

	// Round 1: request + body events, then the streamed reply's tool call.
	cmd := s.Send("", provider.Params{}, req, "q")
	s.Capture(capture.Event{Kind: capture.KindRequest, Status: 200, Method: "POST", Path: "/p"})
	s.Capture(capture.Event{Kind: capture.KindBody, Body: "chunks", BodyBytes: 6})
	drive(t, s, cmd)

	// Round 2 fired inside drive; its transport events arrive afterwards,
	// as the message loop may deliver them.
	s.Capture(capture.Event{Kind: capture.KindRequest, Status: 403, Method: "POST", Path: "/p"})
	s.Capture(capture.Event{Kind: capture.KindBody, Body: `{"statusCode":403}`, BodyBytes: 18})

	recs := s.Records()
	if len(recs) != 2 {
		t.Fatalf("records = %d; want 2 (one per round)", len(recs))
	}
	failed := recs[1]
	if failed.Status != store.Failed {
		t.Errorf("follow-up record status = %v; want Failed", failed.Status)
	}
	if failed.Body.Body != `{"statusCode":403}` {
		t.Errorf("failed record body = %q; want the rejection body kept", failed.Body.Body)
	}
}

// TestFailedSendKeepsItsRecordLine pins the ordering the transcript's
// record line depends on: a send can fail before its request's capture
// event lands (the error delta wins the race through the message loop),
// and the record that materializes afterwards must still find the turn
// that asked for it. Without that, the record exists — the Metrics panel
// and the Inspector both show it — while the message it belongs to has no
// record line under it, which is precisely the row a failed send is worth
// reading.
func TestFailedSendKeepsItsRecordLine(t *testing.T) {
	s := newTestState()
	turn := s.AppendTurn(provider.RoleUser, "hi", store.Complete)
	s.pending, s.sendTurn, s.streaming = turn, turn, true

	// The error arrives first, and its Done ends the send.
	s.Receive(provider.Delta{Err: errors.New("403 Forbidden"), Done: true})
	// Only now does the request's capture event land.
	s.Capture(capture.Event{Kind: capture.KindRequest, Status: 403, Method: "POST", Path: "/v1/chat/completions"})

	rec, ok := s.LastRecord()
	if !ok {
		t.Fatal("no record after the capture event")
	}
	got, ok := s.TurnByID(turn)
	if !ok {
		t.Fatal("the user turn vanished")
	}
	if got.RecordID != rec.ID {
		t.Errorf("user turn RecordID = %d; want %d — the record that arrived "+
			"after the failure never found its turn, so the transcript shows "+
			"no record line under the message", got.RecordID, rec.ID)
	}
}

// TestReplyBeforeRecordKeepsUsage pins the other side of the same race:
// a fast reply's deltas — usage, the assembled body, Done — can reach
// the loop before the request's capture event does. What they said
// about the record must land on it once it exists, or the Metrics
// panel shows dashes for a request that plainly answered.
func TestReplyBeforeRecordKeepsUsage(t *testing.T) {
	s := newTestState()
	turn := s.AppendTurn(provider.RoleUser, "hi", store.Complete)
	s.pending, s.sendTurn, s.streaming = turn, turn, true

	s.Receive(provider.Delta{Content: "priced", Usage: &provider.Usage{Prompt: 1000, Completion: 1000}})
	s.Receive(provider.Delta{Done: true, Final: `{"ok":true}`})
	s.Capture(capture.Event{Kind: capture.KindRequest, Status: 200, Method: "POST", Path: "/v1/chat/completions"})

	rec, ok := s.LastRecord()
	if !ok {
		t.Fatal("no record after the capture event")
	}
	if rec.Usage == nil || rec.Usage.Prompt != 1000 {
		t.Errorf("record usage = %+v; want the usage the delta carried", rec.Usage)
	}
	if rec.Final != `{"ok":true}` {
		t.Errorf("record Final = %q; want the assembled body", rec.Final)
	}
	if rec.Status != store.Complete {
		t.Errorf("record status = %v; want Complete", rec.Status)
	}
}
