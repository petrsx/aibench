// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package selection

// The drag state machine behind text selection. The geometry in
// selection.go turns a screen cell into a position in a pane; this is
// what a press, a motion and a release do with those positions — the
// same three steps on every tab, so they live here once. A tab supplies
// what genuinely differs, as a Target: where its panes are, and how a
// pane repaints once the selection moves.

// PaneNone is "no pane": the zero value of a tab's pane constants, so an
// empty State is a tab with nothing selected.
const PaneNone = 0

// Target is the tab a selection is dragged over.
type Target interface {
	// SelPosAt resolves a screen cell to a position in the given pane, false
	// when the cell is outside it. With clamp set, a cell past the pane's
	// edges is pulled to the nearest position inside — which is what a drag
	// wants and a press does not.
	SelPosAt(pane, x, y int, clamp bool) (Pos, bool)
	// SelRepaint re-derives the pane so the selection it owns (or has just
	// lost) is drawn as it now stands.
	SelRepaint(pane int)
}

// Gesture is what a release turned out to be.
type Gesture int

const (
	// GestureNone: no drag was in progress, so the release means nothing.
	GestureNone Gesture = iota
	// GestureClick: pressed and released on one cell. Not a selection — the
	// clipboard is left alone and the tab reads the click as it likes.
	GestureClick
	// GestureSelect: a real span, worth copying.
	GestureSelect
)

// State is a tab's drag state: which pane owns it, whether the button is
// still down, and the span itself. Anchor is where the press landed, Head
// where the pointer is now — in that order, not sorted, since the direction
// of the drag is what the caller reads.
type State struct {
	Pane   int
	Drag   bool
	Anchor Pos
	Head   Pos
}

// Clear drops the selection without repainting — for a caller that is about
// to re-derive everything anyway (a resize, a new record).
func (s *State) Clear() {
	s.Pane, s.Drag = PaneNone, false
}

// Press starts a drag in the first of panes the cell falls inside, in the
// order given: they are tried innermost-first, so overlapping panes resolve
// the way they are drawn. A press outside all of them clears the selection.
// Both the pane losing the selection and the one taking it are repainted.
func (s *State) Press(t Target, panes []int, x, y int) {
	prev := s.Pane
	s.Clear()
	for _, p := range panes {
		if pos, ok := t.SelPosAt(p, x, y, false); ok {
			s.Pane, s.Drag = p, true
			s.Anchor, s.Head = pos, pos
			break
		}
	}
	if prev != PaneNone && prev != s.Pane {
		t.SelRepaint(prev)
	}
	if s.Pane != PaneNone {
		t.SelRepaint(s.Pane)
	}
}

// Motion extends a live drag to the cell under the pointer, repainting only
// when the head actually moved to a new position.
func (s *State) Motion(t Target, x, y int) {
	if !s.Drag {
		return
	}
	if pos, ok := t.SelPosAt(s.Pane, x, y, true); ok && pos != s.Head {
		s.Head = pos
		t.SelRepaint(s.Pane)
	}
}

// Release ends the drag and says what the gesture was, with the pane it
// happened in. Nothing is repainted here: a click means something different
// on every tab, so the tab acts on the answer and repaints once, when it
// knows what it did. A click also clears the selection — pressing and
// releasing on one cell selects nothing.
func (s *State) Release(t Target, x, y int) (Gesture, int) {
	if !s.Drag {
		return GestureNone, PaneNone
	}
	s.Drag = false
	if pos, ok := t.SelPosAt(s.Pane, x, y, true); ok {
		s.Head = pos
	}
	pane := s.Pane
	if s.Anchor == s.Head {
		s.Pane = PaneNone
		return GestureClick, pane
	}
	return GestureSelect, pane
}
