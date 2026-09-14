// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package store

// Session is the store's serializable content — the seam the persistence
// layer (internal/session) marshals; every field is plain data.
type Session struct {
	Turns   []Turn
	Records []Record
}

// Export snapshots the store for persistence. The slices are copies, so
// a save command can marshal them off the message loop while the store
// keeps mutating.
func (s *Store) Export() Session {
	out := Session{
		Turns:   make([]Turn, len(s.turns)),
		Records: make([]Record, len(s.records)),
	}
	copy(out.Turns, s.turns)
	copy(out.Records, s.records)
	return out
}

// Import replaces the store's content with a loaded session: counters
// continue past the highest ids, the pin resets to follow-latest, and
// the version bumps so every view re-derives.
func (s *Store) Import(data Session) {
	s.turns = make([]Turn, len(data.Turns))
	s.records = make([]Record, len(data.Records))
	copy(s.turns, data.Turns)
	copy(s.records, data.Records)

	s.nextTurnID, s.nextRecordID = 0, 0
	for _, t := range s.turns {
		s.nextTurnID = max(s.nextTurnID, t.ID+1)
	}
	for _, r := range s.records {
		s.nextRecordID = max(s.nextRecordID, r.ID+1)
	}
	s.pinnedID = -1
	s.version++
}
