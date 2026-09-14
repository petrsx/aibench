// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

// The tab chrome every tab is built from: the header box atop the main
// column and the side panel beside it, plus the one definition of the
// column split they share. These are presentation only — what goes in the
// header is the tab's own business (the profile strip on Chat and Prompt,
// the record head on the Inspector), so no tab-specific knowledge lives
// here.

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// TabHeaderRows is the header box's outer height: the border plus one
// content line. Tabs subtract it when sizing their content region.
const TabHeaderRows = 3

// TabHeader renders content as the header box at the given outer width,
// clipping what does not fit — so a caller puts the field that can grow
// arbitrarily long (a URL, a host) last. icon is optional and rides the
// right edge (see CopyLabel); the content is clipped short of it, since a
// truncated URL must not run under a control.
func TabHeader(content, icon string, width int) string {
	w := max(1, width-2)
	room := w
	if icon != "" {
		room = max(1, w-lipgloss.Width(icon)-ControlPad-1)
	}
	row := ansi.Truncate(content, room, "…")
	if icon != "" {
		gap := max(1, w-lipgloss.Width(row)-lipgloss.Width(icon)-ControlPad)
		row += strings.Repeat(" ", gap) + theme.MetaKeyStyle.Render(icon) +
			strings.Repeat(" ", ControlPad)
	}
	return theme.BlurredBorder.Render(lipgloss.NewStyle().Width(w).Render(row))
}

// A control is drawn as a chip: the label with a cell of fill either
// side, in a background that separates it from whatever it sits on. That
// is what makes a run of them read as two things you can press rather
// than one phrase — "raw copy" needed dividers to be legible, a pair of
// chips does not — and the whole chip answers a click, padding included,
// which is a three-cell wider target than the word alone.
const controlPadding = 1

// chipWidth is one control's drawn width, and controlsRun is a whole run
// of them with a cell of daylight between neighbours.
func chipWidth(label string) int { return lipgloss.Width(label) + 2*controlPadding }

func controlsRun(labels ...string) int {
	w := 0
	for i, l := range labels {
		if i > 0 {
			w++ // daylight between chips
		}
		w += chipWidth(l)
	}
	return w
}

// chipCells walks a drawn run and returns each chip's span: the whole
// chip answers a click, padding included, because a button whose edge
// does nothing is a button you have to aim at.
func chipCells(x, y int, labels ...string) []CopyCell {
	out := make([]CopyCell, 0, len(labels))
	for i, l := range labels {
		if i > 0 {
			x++ // daylight between chips
		}
		w := chipWidth(l)
		out = append(out, CopyCell{X: x, Y: y, W: w, Shown: true})
		x += w
	}
	return out
}

// renderControls draws that run.
func renderControls(labels ...string) string {
	var out string
	for i, l := range labels {
		if i > 0 {
			out += " "
		}
		out += theme.ControlStyle.Render(strings.Repeat(" ", controlPadding) + l +
			strings.Repeat(" ", controlPadding))
	}
	return out
}

// PanelControls is a panel's title row with its controls set into the
// top-right corner:
//
//	│ Metrics                    │ copy │ │
//
// The corner is where a reader looks for what a box can do, and the title
// row is chrome — nothing a panel hands to the clipboard comes from it.
// A panel too narrow to hold the run keeps its title alone.
func PanelControls(head string, inner int, labels ...string) string {
	if len(labels) == 0 {
		return head
	}
	run := controlsRun(labels...)
	gap := inner - lipgloss.Width(head) - run - ControlPad
	if gap < 1 {
		return head
	}
	return head + strings.Repeat(" ", gap) + renderControls(labels...) +
		strings.Repeat(" ", ControlPad)
}

// PanelControlCells is where PanelControls put those controls, given the
// panel's top-left cell and outer width — the same arithmetic, so the
// drawing and the click test cannot disagree. The title row is the first
// row inside the box's top border.
func PanelControlCells(x, y, width int, labels ...string) []CopyCell {
	return chipCells(x+width-1-ControlPad-controlsRun(labels...), y+1, labels...)
}

// controlOf turns an optional control into the variadic run the drawing
// helpers take: none when there is no control to draw.
func controlOf(control string) []string {
	if control == "" {
		return nil
	}
	return []string{control}
}

// headerControlsRoom is what a header row leaves its content once the
// controls have taken their right end. Drawing and hit-testing both go
// through it, so a control cannot be drawn where a click is not accepted.
func headerControlsRoom(width int, labels ...string) int {
	return max(1, width-2-ControlPad-controlsRun(labels...))
}

// TabHeaderControls is TabHeader with the pane's controls set into the
// top-right corner of the box:
//
//	╭──────────────────────────────────────────────╮
//	│   Request     Response       │ raw │ copy │  │
//	╰──────────────────────────────────────────────╯
//
// The corner is where a reader looks for what a pane can do, and the
// controls are chrome: they live in the frame, never in the content, so
// what is copied from the pane cannot contain them. The content is
// clipped short of the run — a truncated URL must not run under a
// control.
func TabHeaderControls(content string, width int, labels ...string) string {
	if len(labels) == 0 {
		return TabHeader(content, "", width)
	}
	room := headerControlsRoom(width, labels...)
	row := ansi.Truncate(content, room, "…")
	row += strings.Repeat(" ", max(0, room-lipgloss.Width(row)))
	row += renderControls(labels...) + strings.Repeat(" ", ControlPad)
	return theme.BlurredBorder.Render(lipgloss.NewStyle().Width(max(1, width-2)).Render(row))
}

// TabHeaderControlCells is where TabHeaderControls put those controls,
// given the header box's own top-left cell — the same arithmetic, so the
// drawing and the click test cannot disagree.
func TabHeaderControlCells(x, y, width int, labels ...string) []CopyCell {
	// +1 on y: the box's top border, so the title row is the row below it.
	return chipCells(x+1+headerControlsRoom(width, labels...), y+1, labels...)
}

// MainColumn is the padded wrapper around a tab's frameless main content —
// the transcript, the dump, the rendered prompt — sized from the viewport
// inside it.
//
// The +3 is what the column spends beside the text: the one-column pad on
// the left, then the scrollbar's gutter and the bar. There is no right pad,
// so the bar lands on the column's last cell, in line with the header box's
// right border above it — the chrome reads as one vertical rail instead of
// a bar floating a column short of it. No vertical padding either: content
// sits flush under the header box, as it does flush above whatever closes
// the column (the input box, the rule), so the gaps match.
func MainColumn(content string, vpWidth int) string {
	return lipgloss.NewStyle().Padding(0, 0, 0, 1).Width(vpWidth + 3).Render(content)
}

// ScrollBarFor is WithScrollBar over a viewport's own geometry — the bar
// rides beside the text, for the frameless main column.
func ScrollBarFor(view string, vp viewport.Model) string {
	return WithScrollBar(view, vp.Height(),
		vp.TotalLineCount(), vp.VisibleLineCount(), vp.YOffset())
}

// BorderScrollBarFor is BorderScrollBar over a viewport's own geometry —
// the bar is painted into a box's right border, so it costs no content
// width. firstRow is where the viewport's content starts inside the box.
func BorderScrollBarFor(box string, firstRow int, vp viewport.Model) string {
	return BorderScrollBar(box, firstRow, vp.Height(),
		vp.TotalLineCount(), vp.VisibleLineCount(), vp.YOffset())
}

// SplitWidths is the tab layout's column split — the main column and the
// side panel beside it — in one place, so the three tabs cannot drift out
// of alignment with each other.
func SplitWidths(total int) (main, side int) {
	main = max(3, total*3/4)
	return main, max(3, total-main)
}

// FooterControls is a main column's closing rule with its controls set
// into the right end:
//
//	──────────────────────────────│ raw │ copy │──
//
// The rule is chrome with nothing on it, which is what makes it the place
// for controls: the header box above carries the identity — a profile
// strip, a URL, timings — and every cell a control takes there is a cell
// that truncates.
func FooterControls(width int, labels ...string) string {
	if len(labels) == 0 {
		// A rule with nothing on it is still the column's floor.
		return theme.FrameStyle.Render(strings.Repeat("─", max(1, width)))
	}
	left := max(1, width-controlsRun(labels...)-ControlPad)
	return theme.FrameStyle.Render(strings.Repeat("─", left)) +
		renderControls(labels...) +
		theme.FrameStyle.Render(strings.Repeat("─", ControlPad))
}

// FooterControlCells is where FooterControls put those controls, given the
// rule's own top-left cell — the same arithmetic, so what is drawn and
// what answers a click cannot disagree.
func FooterControlCells(x, y, width int, labels ...string) []CopyCell {
	return chipCells(x+max(1, width-controlsRun(labels...)-ControlPad), y, labels...)
}

// SidePanelTitleRows is how much a titled panel spends above its content:
// the title and the blank row under it. A title hard against the first row
// reads as one of the panel's own headings; the gap makes it the label of
// the box. The Tokens graph has always had it — panels now agree.
const SidePanelTitleRows = 2

// SidePanelRows is how many content rows a panel has when it occupies
// `height` rows on screen: the height less its two border rows and its
// title rows. Tabs size their viewports and clip their cached lines with
// this instead of subtracting the chrome themselves — that arithmetic
// belongs to the panel, and a tab that got it wrong overran the shell's
// help line.
func SidePanelRows(height int) int { return max(1, height-2-SidePanelTitleRows) }

// SidePanel is the bordered, titled panel down a tab's right side, sized to
// the rows it may occupy on screen (SidePanelRows says how many of those
// reach the content). An empty title renders the panel untitled; control
// is optional and is painted into the panel's bottom border, where every
// control in the app lives (see BoxBottomControls). Lines are clipped to
// the panel's inner width by the caller, since they are cached for mouse
// selection.
func SidePanel(title, control string, lines []string, width, height int) string {
	return SidePanelStyled(theme.BlurredBorder, title, control, lines, width, height)
}

// SidePanelStyled is SidePanel with the border style spelled out, for a
// panel that takes focus (the Prompt tab's params).
func SidePanelStyled(border lipgloss.Style, title, control string, lines []string, width, height int) string {
	inner := max(1, width-2)
	body := lipgloss.NewStyle().Width(inner).Height(SidePanelRows(height)).
		Render(strings.Join(lines, "\n"))
	if title == "" {
		return border.Render(body)
	}
	head := PanelControls(theme.PanelTitleStyle.Render(title), inner, controlOf(control)...)
	return border.Render(lipgloss.JoinVertical(lipgloss.Left, head, "", body))
}

// SidePanelWithBar is SidePanel over a viewport, with the scrollbar painted
// into the panel's right border (the Inspector's panes) rather than beside
// the text, so the bar costs no content width. The title is clipped to the
// panel: a long one would otherwise widen the box and push the column past
// the terminal.
func SidePanelWithBar(title, control string, vp viewport.Model) string {
	inner := lipgloss.NewStyle().Width(vp.Width() + 1) // +1: the gutter
	head := theme.PanelTitleStyle.Render(ansi.Truncate(title, max(1, vp.Width()), "…"))
	head = PanelControls(head, vp.Width()+1, controlOf(control)...)
	box := theme.BlurredBorder.Render(inner.Render(lipgloss.JoinVertical(lipgloss.Left,
		head,
		"",
		vp.View(),
	)))
	// Past the top border and the title rows, where the viewport's own
	// content begins.
	return BorderScrollBarFor(box, 1+SidePanelTitleRows, vp)
}
