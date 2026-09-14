// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package find

// The tab side of search. The overlay widget above is only the input; what
// each tab used to keep its own copy of is the behaviour around it — routing
// the overlay's keys, recomputing over fresh content, walking the matches,
// and scrolling the current one into sight. That lives here once, in Pane; a
// tab supplies only what genuinely differs, as a Target.

import (
	tea "charm.land/bubbletea/v2"
)

// Target is the pane a search runs over: where its lines come from, and how
// it repaints once the match set moves. The methods are exported because the
// tabs implement them from their own packages.
type Target interface {
	// FindLines is the content as searched — the tab's clean lines, before
	// any highlight is baked in.
	FindLines() []string
	// FindRepaint re-derives the viewport from those lines with the current
	// matches highlighted, and with nothing highlighted once they are cleared.
	FindRepaint()
	// FindOffset is the viewport's top line, which decides where a fresh
	// query lands.
	FindOffset() int
	// FindReveal scrolls a match into sight.
	FindReveal(Match)
}

// Pane is a tab's whole search state: the overlay and the matches it walks.
type Pane struct {
	Model
	hits []Match
}

// NewPane builds a closed search over a fresh overlay.
func NewPane() Pane { return Pane{Model: New()} }

// Hits are the current matches, in line order — what a tab bakes into its
// lines with Highlight.
func (p *Pane) Hits() []Match { return p.hits }

// Route handles one key while the overlay is open: the widget reports the
// intent, and the target walks or re-derives its highlights.
func (p *Pane) Route(msg tea.KeyPressMsg, t Target) tea.Cmd {
	action, cmd := p.Update(msg)
	switch action {
	case ActionClose:
		p.Dismiss(t)
	case ActionNext:
		p.Step(t, +1)
	case ActionPrev:
		p.Step(t, -1)
	case ActionQueryChanged:
		p.Apply(t)
	}
	return cmd
}

// Dismiss leaves search mode and drops the highlights, reporting whether it
// was open at all — false lets the caller apply its own esc handling instead.
func (p *Pane) Dismiss(t Target) bool {
	if !p.Active() {
		return false
	}
	p.Close()
	p.hits = nil
	t.FindRepaint()
	return true
}

// Refresh recomputes the match set over the target's current lines without
// repainting: content that re-derives itself calls this from inside its own
// render, so a growing reply keeps its highlights aligned. The widget clamps
// the current match into the new set.
func (p *Pane) Refresh(t Target) {
	p.hits = Search(t.FindLines(), p.Query())
	p.SetMatches(len(p.hits))
}

// Apply reacts to a query change: recompute, jump to the match nearest the
// view, repaint, and scroll it into sight.
func (p *Pane) Apply(t Target) {
	p.Refresh(t)
	p.SetCurrent(Nearest(p.hits, t.FindOffset()))
	p.show(t)
}

// Step walks the matches by one, in either direction — shared by the keys and
// the overlay's ▲/▼ icons.
func (p *Pane) Step(t Target, d int) {
	if d < 0 {
		p.Prev()
	} else {
		p.Next()
	}
	p.show(t)
}

// show repaints and brings the current match into view.
func (p *Pane) show(t Target) {
	t.FindRepaint()
	if cur := p.Current(); cur >= 0 && cur < len(p.hits) {
		t.FindReveal(p.hits[cur])
	}
}

// Click walks the matches when one of the overlay's ▲/▼ icons is hit,
// reporting whether it took the click. relX/relY are the click relative to
// the overlay's own origin, which the tab knows and this does not.
func (p *Pane) Click(t Target, relX, relY int) bool {
	if !p.Active() {
		return false
	}
	switch p.HitTest(relX, relY) {
	case ActionNext:
		p.Step(t, +1)
	case ActionPrev:
		p.Step(t, -1)
	default:
		return false
	}
	return true
}
