// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

// An exchange is one question and everything it cost: the first request,
// every tool round the model needed, and the request that finally answered.
// A record is one HTTP call; an exchange is what the user actually asked
// for, and with a tool loop the two differ by a lot — each round resends
// the whole growing context, so the answer costs several times what the
// first request reports.

import (
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/store"
)

// exchangeOf is the records of the exchange containing recordID, in order.
// Exchanges are delimited by the user's turns: everything from one question
// up to the next belongs to it. A record that belongs to no turn (or an
// unknown id) yields nothing.
func (m *Model) exchangeOf(recordID int) []store.Record {
	if recordID < 0 {
		return nil
	}
	var cur, found []int
	seen := map[int]bool{}
	flush := func() {
		if seen[recordID] && found == nil {
			found = cur
		}
		cur, seen = nil, map[int]bool{}
	}
	for _, t := range m.state.Turns() {
		if t.Role == provider.RoleUser {
			flush() // the question opens a new exchange
		}
		if t.RecordID >= 0 && !seen[t.RecordID] {
			seen[t.RecordID] = true
			cur = append(cur, t.RecordID)
		}
	}
	flush()
	if found == nil {
		return nil
	}
	out := make([]store.Record, 0, len(found))
	for _, id := range found {
		if rec, ok := m.state.RecordByID(id); ok {
			out = append(out, rec)
		}
	}
	return out
}
