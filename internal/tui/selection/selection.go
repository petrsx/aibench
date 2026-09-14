// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package selection is mouse text selection over a tab's panes: the
// geometry that turns a screen cell into a position in a pane's cached
// lines (this file), the drag state machine every tab runs the same
// way (drag.go), and what a selected span yields — the styled repaint
// and the clipboard text. A tab supplies only what differs: where its
// panes are and how one repaints (Target).
package selection

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// Pos is a cell-precise position in a pane's rendered content: Line
// indexes the cached content lines, Col is a terminal cell column.
type Pos struct {
	Line, Col int
}

// Pane is a selectable pane's geometry, as the tab knows it: where it sits
// on screen, how much of it is visible, how far its viewport is scrolled,
// and the cached content lines the selection indexes. The tab owns the
// per-pane lookup (its panes differ); the mapping below is shared, so a
// fix to it lands on every tab at once.
type Pane struct {
	Top, Left     int // screen row/column of the pane's first content cell
	Width, Height int // visible content size, in cells
	YOffset       int // the viewport's scroll offset
	Lines         []string
}

// PaneAt maps screen coordinates to a position in the pane's content. With
// clamp, coordinates outside the pane snap to the nearest cell (a drag that
// leaves the pane keeps selecting); without, they report ok=false. A pane
// with no content never matches.
func PaneAt(p Pane, x, y int, clamp bool) (Pos, bool) {
	if len(p.Lines) == 0 {
		return Pos{}, false
	}
	bottom := p.Top + p.Height - 1
	right := p.Left + p.Width - 1
	if !clamp && (y < p.Top || y > bottom || x < p.Left || x > right) {
		return Pos{}, false
	}
	y = min(max(y, p.Top), bottom)
	x = min(max(x, p.Left), right)
	line := min(max(p.YOffset+y-p.Top, 0), len(p.Lines)-1)
	return Pos{Line: line, Col: x - p.Left}, true
}

// Order returns the two positions in reading order.
func Order(a, b Pos) (Pos, Pos) {
	if a.Line > b.Line || (a.Line == b.Line && a.Col > b.Col) {
		return b, a
	}
	return a, b
}

// Span returns the cell range selected on content line i, or ok=false
// when the line is outside the selection.
func Span(i int, a, b Pos, plainWidth int) (from, to int, ok bool) {
	if i < a.Line || i > b.Line {
		return 0, 0, false
	}
	from, to = 0, plainWidth
	if i == a.Line {
		from = min(a.Col, plainWidth)
	}
	if i == b.Line {
		to = min(b.Col+1, plainWidth)
	}
	if from >= to {
		return 0, 0, false
	}
	return from, to, true
}

// Copy returns the selected text across lines, ANSI-stripped and with
// each line's trailing spaces trimmed — what goes on the clipboard.
func Copy(lines []string, anchor, head Pos) string {
	a, b := Order(anchor, head)
	var parts []string
	for i := a.Line; i <= b.Line && i < len(lines); i++ {
		plain := ansi.Strip(lines[i])
		w := lipgloss.Width(plain)
		from, to, ok := Span(i, a, b, w)
		seg := ""
		if ok {
			seg = ansi.Cut(plain, from, to)
		}
		parts = append(parts, strings.TrimRight(seg, " "))
	}
	return strings.Join(parts, "\n")
}

// Apply re-renders the selected span of lines with the selection
// style. The unselected parts keep their original styling: ansi.Cut is
// escape-aware and carries SGR state across the cut points, with explicit
// resets fencing the selection span off from leaked attributes.
func Apply(lines []string, anchor, head Pos) []string {
	a, b := Order(anchor, head)
	out := make([]string, len(lines))
	copy(out, lines)
	for i := a.Line; i <= b.Line && i < len(out); i++ {
		styled := out[i]
		plain := ansi.Strip(styled)
		w := lipgloss.Width(plain)
		from, to, ok := Span(i, a, b, w)
		if !ok {
			continue
		}
		out[i] = ansi.Cut(styled, 0, from) + ansi.ResetStyle +
			theme.SelectionStyle.Render(ansi.Cut(plain, from, to)) +
			ansi.Cut(styled, to, w) + ansi.ResetStyle
	}
	return out
}
