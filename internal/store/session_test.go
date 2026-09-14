// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package store

import (
	"testing"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/provider"
)

// TestExportImportRoundTrip pins the persistence seam: an exported
// session re-imports with content intact, counters continuing past the
// loaded ids, the pin reset, and the version bumped.
func TestExportImportRoundTrip(t *testing.T) {
	s := New(10)
	id := s.AppendTurn(provider.RoleUser, "hello", Complete)
	s.AppendTurn(provider.RoleAssistant, "hi there", Complete)
	rec := s.BeginRecord(capture.Event{Method: "POST", Path: "/x"})
	s.Link(id, rec)
	s.Pin(rec)

	snap := s.Export()
	// The snapshot is a copy: later mutations must not leak into it.
	s.AppendTurn(provider.RoleUser, "after the snapshot", Complete)
	if len(snap.Turns) != 2 {
		t.Fatalf("snapshot turns = %d; want 2 (copy, not view)", len(snap.Turns))
	}

	fresh := New(10)
	before := fresh.Version()
	fresh.Import(snap)
	if fresh.Version() == before {
		t.Error("Import must bump the version")
	}
	if got := fresh.Turns(); len(got) != 2 || got[0].Content != "hello" {
		t.Fatalf("imported turns = %+v; want the snapshot's two", got)
	}
	if fresh.Pinned() != -1 {
		t.Error("Import must reset the pin to follow-latest")
	}
	// Counters continue past the imported ids.
	next := fresh.AppendTurn(provider.RoleUser, "new", Complete)
	for _, turn := range fresh.Turns()[:2] {
		if turn.ID == next {
			t.Fatalf("new turn id %d collides with an imported one", next)
		}
	}
}
