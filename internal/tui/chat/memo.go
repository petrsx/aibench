// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

// The transcript's derived-content cache. Deciding a body is JSON,
// colorizing it, and sizing up a folded payload are pure functions of a
// turn's content — but the transcript is re-derived on every frame while a
// reply streams, so without a memo the session re-parses every payload it
// has ever shown ten times a second. That was the bulk of a render
// (BenchmarkRenderChat: json.Unmarshal alone came to a quarter of it).
//
// Content only moves while a turn is still in progress, so an entry is keyed
// by the turn and validated by the length it was derived at.

import (
	"github.com/petrsx/aibench/internal/store"
	"github.com/petrsx/aibench/internal/tui/render"
)

// derived is one turn's cached presentation. The have-flags matter: a turn
// is asked for whichever of these its row needs, and nothing else is paid
// for — a folded payload never gets colorized, an unopened one never gets
// parsed twice.
type derived struct {
	size int // content length the entry was derived at

	haveJSON bool
	isJSON   bool
	pretty   []string // the colorized body; nil until asked for
	summary  string   // the folded payload's one-line size-up; "" until asked
}

// entry is the turn's cache slot, rebuilt when its content has moved.
func (m *Model) entry(t store.Turn) *derived {
	if d := m.derived[t.ID]; d != nil && d.size == len(t.Content) {
		return d
	}
	d := &derived{size: len(t.Content)}
	m.derived[t.ID] = d
	return d
}

// isJSONBody reports whether the turn's content renders with token colors
// rather than as prose.
func (m *Model) isJSONBody(t store.Turn) bool {
	d := m.entry(t)
	if !d.haveJSON {
		d.isJSON, d.haveJSON = render.IsJSONContent(t.Content), true
	}
	return d.isJSON
}

// prettyBody is the turn's content re-indented and colorized.
func (m *Model) prettyBody(t store.Turn) []string {
	d := m.entry(t)
	if d.pretty == nil {
		d.pretty = render.PrettyBody(t.Content)
	}
	return d.pretty
}

// payloadSummary sizes up a collapsed tool result for its ⎿ header.
func (m *Model) payloadSummary(t store.Turn) string {
	d := m.entry(t)
	if d.summary == "" {
		d.summary = toolSummary(t.Content)
	}
	return d.summary
}

// forgetDerived drops the whole cache when the turn id space goes backwards,
// which is what a cleared (or replaced) session looks like from here: ids
// restart at 0, so an entry keyed by one would answer for a different turn.
func (m *Model) forgetDerived(turns []store.Turn) {
	top := -1
	if n := len(turns); n > 0 {
		top = turns[n-1].ID // turns are appended in id order
	}
	if top < m.derivedTop {
		clear(m.derived)
	}
	m.derivedTop = top
}
