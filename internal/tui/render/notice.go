// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// noticePad is the cell either side of a notice's text — the daylight
// that keeps it off the content, and the width it costs.
const noticePad = 2

// WithNotice composites notice over a pane's bottom-right corner — a
// real lipgloss layer, anchoring transient notices ("copied to clipboard")
// without disturbing the layout. Layers must ride a Compositor: a bare
// Layer.Draw is only a content stamp at the origin (position/children/z all
// live in the Compositor).
//
// The notice is padded a cell either side, and the pad is what makes it
// readable: those cells belong to the layer, so they blank the text
// underneath instead of letting a sentence run straight into the notice.
// The block itself runs flush to the right edge of whatever it is drawn
// over — the daylight the reader sees is the pad, inside it.
func WithNotice(block, notice string, width, height int) string {
	return WithNoticeIn(block, notice, width, height, width)
}

// ScrollBarCols is what a column's scrollbar occupies at its right edge:
// the rail itself. A notice ends before it — the bar says where the
// reader is in what they are reading, and covering that to say "copied"
// trades a fact for a confirmation.
const ScrollBarCols = 1

// WithNoticeIn is WithNotice ending at a column short of the block's own
// right edge — for a column whose last cells are not content: the
// scrollbar rides there, and a notice painted over the rail hides where
// the reader is in what they are reading.
func WithNoticeIn(block, notice string, width, height, right int) string {
	// A notice never widens the pane it floats over: it is a glance at
	// content, and one that runs off the edge is worse than a shortened
	// one — the end of a sentence is where the reader learns it was cut.
	// ANSI-aware, since the caller hands over styled text.
	if avail := min(right, width) - noticePad; avail > 0 {
		notice = ansi.Truncate(notice, avail, "…")
	}
	chip := lipgloss.NewStyle().Padding(0, 1).Render(notice)
	canvas := lipgloss.NewCanvas(max(1, width), max(1, height))
	canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(block),
		// Flush with the anchor: the chip carries its own pad, so the text
		// keeps its daylight while the block runs out to the edge it was
		// given — a chip floating short of it reads as misplaced.
		lipgloss.NewLayer(chip).
			X(max(0, min(right, width)-lipgloss.Width(chip))).
			Y(max(0, height-1)).
			Z(1),
	))
	return canvas.Render()
}

// WithFind composites the find overlay over a pane's top-right corner, the
// sibling of WithNotice (bottom-right) — named for its purpose so it reads
// instinctively where a tab wraps it. The overlay's screen origin, for the
// caller's click/cursor math, is (width - overlayWidth, 0).
func WithFind(block, overlay string, width, height int) string {
	canvas := lipgloss.NewCanvas(max(1, width), max(1, height))
	canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(block),
		lipgloss.NewLayer(overlay).
			X(max(0, width-lipgloss.Width(overlay))).
			Y(0).
			Z(1),
	))
	return canvas.Render()
}

// WithDialog composites a modal dialog centered over the frame, the
// third sibling (WithNotice bottom-right, WithFind top-right).
func WithDialog(block, overlay string, width, height int) string {
	canvas := lipgloss.NewCanvas(max(1, width), max(1, height))
	canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(block),
		lipgloss.NewLayer(overlay).
			X(max(0, (width-lipgloss.Width(overlay))/2)).
			Y(max(0, (height-lipgloss.Height(overlay))/3)).
			Z(1),
	))
	return canvas.Render()
}
