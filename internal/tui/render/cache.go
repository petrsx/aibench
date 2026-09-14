// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

// Cached content lines. A tab caches the lines it renders and indexes them
// by mouse position (selection.PaneAt), so the cache must be shaped
// exactly like what reaches the screen: fit the lines here, at cache time,
// never on the way out. The two shapes a pane needs — wrapped into its
// column, or clipped to it — are siblings so the invariant is stated once.

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Wrap fits lines to a pane's width, with no pad of its own — for content
// in the main column, which supplies the pad itself (MainColumn). Content
// caches must never add a second one: a pane whose text starts two columns
// in when its neighbour starts at one is the mismatch you see first.
func Wrap(lines []string, width int) []string {
	if len(lines) == 0 {
		return nil
	}
	wrap := lipgloss.NewStyle().Width(max(1, width))
	return strings.Split(wrap.Render(strings.Join(lines, "\n")), "\n")
}

// WrapIndent is Wrap that keeps the shape of what it wraps: a line too
// long for the pane continues at its own depth plus two, instead of
// restarting at column zero and flattening every nesting level into the
// same margin. Structure is most of what a body dump is for.
//
// The indent is display only. It is injected into the cached lines, so
// anything copied from them carries it — which is why the Inspector's copy
// control hands over the body as received rather than the pane as drawn.
func WrapIndent(lines []string, width int) []string {
	var out []string
	for _, l := range lines {
		plain := ansi.Strip(l)
		indent := len(plain) - len(strings.TrimLeft(plain, " "))
		if indent > width/2 { // a line indented past half the pane hangs at zero
			indent = 0
		}
		hang := strings.Repeat(" ", indent+2)
		body := ansi.Cut(l, indent, ansi.StringWidth(plain))
		wrapped := strings.Split(ansi.Wrap(body, max(1, width-indent-2), ""), "\n")
		for i, piece := range wrapped {
			if i == 0 {
				out = append(out, strings.Repeat(" ", indent)+piece)
				continue
			}
			out = append(out, hang+piece)
		}
	}
	return out
}

// Inset is Wrap plus a one-column pad — for a side panel's content, which
// sits directly inside the panel border and has no pad of its own.
func Inset(lines []string, width int) []string {
	var out []string
	for _, l := range Wrap(lines, width-1) {
		out = append(out, " "+l)
	}
	return out
}

// InsetClip is Inset for a pane whose rows are one line each: the pad,
// then the row cut to what is left, marked. Wrapping a list of
// name-and-value rows turns every long one into two, and a pane of
// two-line rows is a pane you count rather than scan.
func InsetClip(lines []string, width int) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, " "+ansi.Truncate(l, max(1, width-1), "…"))
	}
	return out
}

// Clip truncates each line to width, marking what was cut — a pane whose
// rows are one line each and must not wrap (the Metrics panel). The input
// slice is modified in place and returned.
func Clip(lines []string, width int) []string {
	w := max(1, width)
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], w, "…")
	}
	return lines
}
