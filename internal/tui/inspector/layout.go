// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package inspector

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// Geometry and chrome: SetSize carves the record head, the body dump,
// and the right column out of the content region; the render helpers
// below draw the boxes and rules the panes share — the mirror of
// chat/layout.go.

// ApplyTheme re-bakes the dump so the find highlights pick up the new palette
// (the shell calls it when the palette flips light/dark).
func (m *Model) ApplyTheme() {
	m.refreshDump()
}

// metricsWidth is the side column's width, from the shared split — so the
// Inspector's headers/sizes column lines up with the other tabs'.
func (m *Model) metricsWidth() int {
	_, side := render.SplitWidths(m.width)
	return side
}

// SetSize lays the panes out within the content region the shell allots
// (top is the screen row the region begins at; contentHeight excludes the
// header and help chrome).
func (m *Model) SetSize(top, width, contentHeight int) {
	// A geometry change re-wraps content, so a kept selection's line indexes
	// would point at the wrong text.
	m.ClearSelection()
	m.top, m.width = top, width
	// Left column: the content floats — no border, matching the Chat tab.
	// Width is the column less a one-column pad, the scrollbar's gutter and
	// the bar itself. Height is everything between the head strip (sub-tabs
	// + rule) and the rule that closes the content, so the body — and its
	// scrollbar — reach the bottom of the tab.
	m.dump.SetWidth(max(1, width-m.metricsWidth()-3))
	m.dump.SetHeight(max(1, contentHeight-headRows-1))
	m.contentH = contentHeight
	// -2 border, -1 gutter before the border-painted scrollbar.
	m.headers.SetWidth(max(1, m.metricsWidth()-3))
	m.sizes.SetWidth(max(1, m.metricsWidth()-3))
	m.layoutRight()
	m.Refresh()
}

// sizesMinBox is the shortest useful Request size pane: fewer content rows
// than this and the split costs more than it shows.
const sizesMinBox = 4

// layoutRight splits the right column. Request size is a request-only pane
// and only appears when there is a breakdown to show, so it folds away on
// the response sub-view and on any request whose body carries no turn list
// — an empty box titled for content that cannot appear is worse than no
// box. The same fold happens on a short terminal, where two halves would
// both be unusably small. Re-run whenever the sub-view, the content, or the
// geometry changes.
func (m *Model) layoutRight() {
	// The viewports get what the panel leaves after its own chrome —
	// render.SidePanelRows owns that arithmetic, so a box can never grow
	// past the column and push the shell's help line off screen.
	half := m.contentH / 2
	if m.view != viewRequest || !m.hasSizes || render.SidePanelRows(half) < sizesMinBox {
		m.headers.SetHeight(render.SidePanelRows(m.contentH))
		m.sizes.SetHeight(0)
		return
	}
	m.headers.SetHeight(render.SidePanelRows(m.contentH - half))
	m.sizes.SetHeight(render.SidePanelRows(half))
}

// ClearSelection drops any kept highlight (e.g. on a resize/layout change).
func (m *Model) ClearSelection() {
	m.sel.Pane, m.sel.Drag = paneNone, false
}

// The Inspector's side panes are the shared render.SidePanelWithBar (a
// titled panel with the scrollbar painted into its right border, so the bar
// costs no content width). The dump has no border to paint on — its bar
// rides beside the text instead.

// viewHead is the single-line strip at the top of the body box: the
// request/response sub-tabs followed by the shown record's identity, URL, and
// timings, clipped to the box's inner width.
func (m *Model) viewHead(tabsRow string) string {
	line := tabsRow
	if rec, ok := m.state.ShownRecord(); ok {
		id := theme.DimStyle.Render(rec.Time.Format("15:04:05")) + " "
		if rec.Req.Method == "" {
			line += "   " + id + theme.ErrorStyle.Render("transport failure")
		} else {
			info := id + theme.MethodStyle.Render(rec.Req.Method) + " " +
				render.StatusStyle(rec.Req.Status).Render(strconv.Itoa(rec.Req.Status))
			if m.state.Pinned() >= 0 {
				info += theme.DimStyle.Render(" · pinned")
			}
			url := rec.Req.Path
			if rec.Req.Query != "" {
				url += "?" + rec.Req.Query
			}
			timings := fmt.Sprintf("%s ttfb", render.Elapsed(rec.Req.Duration))
			if rec.Req.ReqBytes > 0 {
				// The sent size diagnoses gateway size limits (e.g. a
				// content-safety policy failing closed on large prompts).
				timings += fmt.Sprintf(" · ↑ %s", render.Bytes(rec.Req.ReqBytes))
			}
			if rec.Body.Kind == capture.KindBody {
				timings += fmt.Sprintf(" · %s total · %s body", render.Elapsed(rec.Body.Duration), render.Bytes(rec.Body.BodyBytes))
			}
			sep := theme.DimStyle.Render(" · ")
			line += "   " + info + sep + theme.DimStyle.Render(url) + sep + theme.DimStyle.Render(timings)
		}
	}
	return ansi.Truncate(line, max(1, m.dump.Width()), "…")
}
