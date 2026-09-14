// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package store is the session database: chat turns and request records.
// The TUI renders from it; the store itself renders nothing. It is owned
// by the Bubble Tea model and mutated only inside its message loop, so it
// needs no locking.
package store

import (
	"time"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/provider"
)

// Status is the lifecycle of a turn or record.
type Status int

const (
	InProgress Status = iota // streaming / awaiting the response body
	Complete
	Failed
)

// RoleError marks display-only error bubbles, RoleNotice display-only
// notices (e.g. a user interrupt); neither is ever sent back to the API.
const (
	RoleError  = "error"
	RoleNotice = "notice"
)

// Turn is one chat entry.
type Turn struct {
	ID       int
	Role     string // provider.RoleUser / provider.RoleAssistant / RoleError
	Content  string
	Status   Status
	RecordID int // record this turn triggered; -1 none
	// Total is the send's wall-clock from dispatch to completion, spanning
	// any 429 retries/backoff; 0 until set. It exceeds the linked record's
	// own latency exactly when the request didn't succeed on the first try.
	Total time.Duration
	// ToolCalls are the tool invocations an assistant turn requested;
	// replayed to the API when the history is re-sent.
	ToolCalls []provider.ToolCall
	// ToolCallID links a provider.RoleTool turn's result to its call;
	// ToolName is that call's tool, denormalized for display.
	ToolCallID string
	ToolName   string
}

// Record is one HTTP request/response.
type Record struct {
	ID     int
	Time   time.Time     // request time; event time for transport failures
	Req    capture.Event // KindRequest; zero for transport failures
	Body   capture.Event // KindBody stats; raw text dropped once Final is set
	Final  string        // assembled response body the Inspector shows (empty on errors)
	Usage  *provider.Usage
	Err    string // condensed error text on failed records
	Status Status
}

// Store holds the session.
type Store struct {
	turns   []Turn
	records []Record

	nextTurnID   int
	nextRecordID int
	maxRecords   int
	version      int

	pinnedID int // record the diagnostics views show; -1 follows the latest
}

// New builds an empty store bounding the record history.
func New(maxRecords int) *Store {
	return &Store{maxRecords: max(1, maxRecords), pinnedID: -1}
}

// Version increments on every mutation; views memoize renders against it.
func (s *Store) Version() int { return s.version }

// --- turns ---------------------------------------------------------------

// AppendTurn adds a chat entry and returns its id.
func (s *Store) AppendTurn(role, content string, status Status) int {
	s.version++
	t := Turn{ID: s.nextTurnID, Role: role, Content: content, Status: status, RecordID: -1}
	s.nextTurnID++
	s.turns = append(s.turns, t)
	return t.ID
}

// AppendToTurn appends streamed content to a turn.
func (s *Store) AppendToTurn(id int, delta string) {
	if t := s.turn(id); t != nil {
		s.version++
		t.Content += delta
	}
}

// SetToolCalls records the tool invocations an assistant turn requested.
func (s *Store) SetToolCalls(id int, calls []provider.ToolCall) {
	if t := s.turn(id); t != nil {
		s.version++
		t.ToolCalls = calls
	}
}

// AppendToolResult adds a tool-result turn answering callID and returns
// its id. Tool results are part of the conversation — they are sent back
// to the API, unlike error/notice bubbles.
func (s *Store) AppendToolResult(callID, name, content string) int {
	s.version++
	t := Turn{
		ID:         s.nextTurnID,
		Role:       provider.RoleTool,
		Content:    content,
		Status:     Complete,
		RecordID:   -1,
		ToolCallID: callID,
		ToolName:   name,
	}
	s.nextTurnID++
	s.turns = append(s.turns, t)
	return t.ID
}

// SetTotal records a send's total wall-clock on its turn.
func (s *Store) SetTotal(id int, d time.Duration) {
	if t := s.turn(id); t != nil {
		s.version++
		t.Total = d
	}
}

// CompleteTurn marks a turn finished.
func (s *Store) CompleteTurn(id int) {
	if t := s.turn(id); t != nil {
		s.version++
		t.Status = Complete
	}
}

// Turns is the chat history, oldest first. The slice is the store's own;
// treat it as read-only.
func (s *Store) Turns() []Turn { return s.turns }

// TurnByID resolves a turn.
func (s *Store) TurnByID(id int) (Turn, bool) {
	if t := s.turn(id); t != nil {
		return *t, true
	}
	return Turn{}, false
}

func (s *Store) turn(id int) *Turn {
	for i := len(s.turns) - 1; i >= 0; i-- {
		if s.turns[i].ID == id {
			return &s.turns[i]
		}
	}
	return nil
}

// --- records -------------------------------------------------------------

// BeginRecord opens a record for a request that was actually sent.
func (s *Store) BeginRecord(req capture.Event) int {
	s.version++
	r := Record{
		ID:     s.nextRecordID,
		Time:   req.Timestamp(),
		Req:    req,
		Status: InProgress,
	}
	s.nextRecordID++
	s.records = append(s.records, r)
	s.trim()
	return r.ID
}

// FailRecord adds a standalone failed record for a request that never got
// a response (transport errors).
func (s *Store) FailRecord(text string) int {
	s.version++
	r := Record{
		ID:     s.nextRecordID,
		Time:   time.Now(),
		Err:    text,
		Status: Failed,
	}
	s.nextRecordID++
	s.records = append(s.records, r)
	s.trim()
	return r.ID
}

// FinishBody attaches the response body stats to the newest record. When the
// assembled Final is already in (the streamed happy path), the raw stream text
// is dropped — only its stats are kept — since the chunks would just cost
// memory. Error responses have no Final, so their raw body survives to be shown.
func (s *Store) FinishBody(body capture.Event) {
	if r := s.last(); r != nil {
		s.version++
		if r.Final != "" {
			body.Body = ""
		}
		r.Body = body
	}
}

// SetFinal attaches the assembled response body to the newest record and drops
// any raw stream text already captured; the two arrive on independent channels,
// so this and FinishBody cover both orderings. Body stats are left intact.
func (s *Store) SetFinal(final string) {
	if r := s.last(); r != nil {
		s.version++
		r.Final = final
		r.Body.Body = ""
	}
}

// SetUsage attaches token usage to the newest record.
func (s *Store) SetUsage(u *provider.Usage) {
	if r := s.last(); r != nil {
		s.version++
		r.Usage = u
	}
}

// FailCurrent marks the newest in-progress record failed with the given
// condensed error text.
func (s *Store) FailCurrent(text string) {
	if r := s.last(); r != nil && r.Status == InProgress {
		s.version++
		r.Status = Failed
		r.Err = text
	}
}

// FinishRecord closes the newest record: Complete, or Failed for error
// statuses.
func (s *Store) FinishRecord() {
	r := s.last()
	if r == nil || r.Status != InProgress {
		return
	}
	s.version++
	if r.Req.Status >= 400 {
		r.Status = Failed
		return
	}
	r.Status = Complete
}

// Link ties a turn to the record it triggered.
func (s *Store) Link(turnID, recordID int) {
	if t := s.turn(turnID); t != nil {
		s.version++
		t.RecordID = recordID
	}
}

// Records is the request history, oldest first. The slice is the store's
// own; treat it as read-only.
func (s *Store) Records() []Record { return s.records }

// LastRecord returns the newest record.
func (s *Store) LastRecord() (Record, bool) {
	if r := s.last(); r != nil {
		return *r, true
	}
	return Record{}, false
}

// RecordByID resolves a record; ids stay valid across trimming (the
// record may just be gone).
func (s *Store) RecordByID(id int) (Record, bool) {
	for i := len(s.records) - 1; i >= 0; i-- {
		if s.records[i].ID == id {
			return s.records[i], true
		}
	}
	return Record{}, false
}

// --- selection -----------------------------------------------------------

// Pin fixes which record the diagnostics views (Metrics, Inspector) show;
// -1 follows the latest. Records own their selection so the tabs share only
// the store, not a separate selection state.
func (s *Store) Pin(id int) {
	if s.pinnedID == id {
		return
	}
	s.version++
	s.pinnedID = id
}

// Pinned is the pinned record id, or -1 while following the latest.
func (s *Store) Pinned() int { return s.pinnedID }

// StepPin walks the pin by whole records — the keyboard's version of
// clicking a record line, and the same walk in every view that has one. It
// starts from what is being shown (so the first step from the latest moves
// off it) and stops at the ends: wrapping from the newest to the oldest
// would move the reader somewhere they did not ask to go. Reports whether
// the pin moved.
func (s *Store) StepPin(by int) bool {
	if len(s.records) == 0 {
		return false
	}
	at := len(s.records) - 1
	if shown, ok := s.ShownRecord(); ok {
		for i, r := range s.records {
			if r.ID == shown.ID {
				at = i
				break
			}
		}
	}
	next := min(max(at+by, 0), len(s.records)-1)
	if next == at {
		return false
	}
	// Walking onto the newest releases the pin rather than fixing it to
	// that record: a reader who has come back to the end wants the views
	// to keep up with what arrives next, which is what no pin means.
	if next == len(s.records)-1 {
		s.Pin(-1)
		return true
	}
	s.Pin(s.records[next].ID)
	return true
}

// ShownRecord is the record the diagnostics views display: the pinned one
// while it still resolves, otherwise the newest.
func (s *Store) ShownRecord() (Record, bool) {
	if s.pinnedID >= 0 {
		if r, ok := s.RecordByID(s.pinnedID); ok {
			return r, true
		}
	}
	return s.LastRecord()
}

func (s *Store) last() *Record {
	if len(s.records) == 0 {
		return nil
	}
	return &s.records[len(s.records)-1]
}

func (s *Store) trim() {
	if cut := len(s.records) - s.maxRecords; cut > 0 {
		s.records = s.records[cut:]
	}
}
