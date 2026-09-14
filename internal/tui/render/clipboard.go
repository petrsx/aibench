// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

// Copying to the clipboard, for any surface that has something worth
// taking: a panel of metrics, a dump of headers, a rendered prompt. The
// label, the hit box and the text-shaping are here so a copy control means
// the same thing wherever it appears — and so adding one to a new pane is
// three lines rather than a small design decision.
//
// The clipboard write itself is notify.Notification.Copy: a copy that says
// nothing looks like a click that missed.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// CopyLabel is the control, painted into the bottom border of whatever box
// it belongs to. In the border, because a control among values would be
// mistaken for one and would sit in the way of selecting the values around
// it — and because the frame is never handed to the clipboard.
//
// It is a word, not an icon. A terminal draws every glyph in one cell at
// the user's font size, so a "bigger icon" is not available: the copy
// marks (⧉ ❐ ⎘) all come out as faint line art a few pixels across. A word
// is legible at any size, says what it does without being learned, and
// gives the pointer four cells to land on instead of one.
const CopyLabel = "copy"

// ControlPad is the gap kept between a control run and the corner to its
// right: a control hard against the corner reads as part of it, and is a
// worse target for being in the corner.
const ControlPad = 1

// CopyCell is where one control sits on screen. The zero value is nowhere,
// so a surface that draws no control hit-tests to false without a special
// case.
type CopyCell struct {
	X, Y  int
	W     int // cells the control occupies, from X
	Shown bool
}

// Hit reports whether a click landed on the control.
func (c CopyCell) Hit(x, y int) bool {
	return c.Shown && y == c.Y && x >= c.X && x < c.X+c.W
}

// CopyText is what a pane hands over: its cached lines as plain text, in
// the shape they are shown, without the styling that would paste as escape
// codes or the padding that would paste as trailing spaces.
func CopyText(lines []string) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, strings.TrimRight(ansi.Strip(l), " "))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}
