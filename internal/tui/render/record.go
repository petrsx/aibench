// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// RequestSummary renders the method/path/status/duration line plus the
// spec-resolved meta segment for one request event, the core of a turn's
// record line.
func RequestSummary(spec provider.Spec, e capture.Event) (line, meta string) {
	path := strings.Replace(e.Path, "/openai/deployments/", "…/", 1)
	line = fmt.Sprintf("%s %s %s %s",
		theme.MethodStyle.Render(e.Method),
		path,
		StatusStyle(e.Status).Render(fmt.Sprintf("%d", e.Status)),
		theme.DimStyle.Render(e.Duration.String()),
	)
	if rows := provider.Resolve(spec.LogMeta, e.RespHeaders); len(rows) > 0 {
		meta = RenderMeta(rows)
	}
	return line, meta
}

// StatusStyle picks the status color: red for 5xx, amber for 4xx, green else.
func StatusStyle(status int) lipgloss.Style {
	switch {
	case status >= 500:
		return theme.ErrorStatus
	case status >= 400:
		return theme.WarningStatus
	default:
		return theme.OKStatus
	}
}

// RenderMeta renders key=value meta segments (request ids, rate limits).
func RenderMeta(meta []capture.KeyValue) string {
	parts := make([]string, 0, len(meta))
	for _, kv := range meta {
		parts = append(parts, theme.MetaKeyStyle.Render(kv.Key+"=")+theme.DimStyle.Render(kv.Value))
	}
	return strings.Join(parts, "  ")
}

// RenderUsage renders token counts total-first with the split as an
// aside — "22 tokens (↑15 · ↓7)" — the arrows and colors matching the
// Tokens graph legend so the numbers read as the same data everywhere.
func RenderUsage(u *provider.Usage) string {
	return theme.UsageTotalStyle.Render(strconv.Itoa(u.Total)) +
		theme.MetaKeyStyle.Render(" tokens ") +
		theme.DimStyle.Render("(") +
		theme.TokensInStyle.Render(fmt.Sprintf("↑%d", u.Prompt)) +
		theme.DimStyle.Render(" · ") +
		theme.TokensOutStyle.Render(fmt.Sprintf("↓%d", u.Completion)) +
		theme.DimStyle.Render(")")
}

// RecordRow composes one record line: the branch glyph and the request
// summary, truncated to fit. The whole row is a click target (the caller
// tags it in lineRec) — which record is *shown* is said by the rule down
// its group's left edge, not here: a record is an exchange, and marking
// only its summary row could never show how far the exchange reaches.
func RecordRow(summary string, width int) string {
	// Claude Code's result-attachment glyph; the extra space keeps a
	// visible gap on terminals that draw the glyph wide.
	const marker = "  ⎿  "
	room := max(1, width-lipgloss.Width(marker)-1)
	return theme.DimStyle.Render(marker) + ansi.Truncate(summary, room, "…")
}

// NiceCeil rounds up to a rounded scale bound: 10, 20, 50, 100, 200, 500…
// ScrollBar draws a vertical scrollbar column: h rows of track with a thumb
// sized to the visible fraction and positioned by the offset. When
// everything fits it returns blanks rather than nothing, so a pane can
// reserve the column unconditionally and its width never shifts as content
// grows — a width that changed with content would re-wrap the cached lines
// mouse selection maps against.
// Scrollbar glyphs. A terminal cell is the narrowest a bar can be, so
// "thinner" is a matter of weight: a light vertical rule for the track and a
// heavy one for the thumb read as a slim bar, where a full block (█) reads
// as a wall. Kept together so the pair can be re-weighted in one place.
const (
	scrollTrack = "│" // a light rule: part of the frame, not content
	// Half-block: heavier than the track so the moving part reads at a
	// glance, without the wall a full or three-quarter block makes. The
	// block ladder ▏▎▍▌▋▊▉█ is the dial if this wants re-weighting.
	scrollThumb = "▌"
)

func ScrollBar(h, total, visible, offset int) []string {
	if h <= 0 {
		return nil
	}
	out := make([]string, h)
	if total <= visible || visible <= 0 {
		for i := range out {
			out[i] = " "
		}
		return out
	}
	thumb := max(1, int(math.Round(float64(h)*float64(visible)/float64(total))))
	thumb = min(thumb, h)
	span := total - visible
	pos := 0
	if span > 0 {
		pos = int(math.Round(float64(h-thumb) * float64(offset) / float64(span)))
	}
	pos = min(max(pos, 0), h-thumb)
	for i := range out {
		if i >= pos && i < pos+thumb {
			out[i] = scrollThumb
		} else {
			out[i] = scrollTrack
		}
	}
	return out
}

// WithScrollBar joins a rendered pane with its scrollbar column on the
// right. Pass the viewport's own numbers: height, TotalLineCount,
// VisibleLineCount, YOffset. The column replaces the scroll percentage the
// panes used to carry in a footer — it says the same thing continuously
// instead of as a number, costs a column instead of a row, and shows how
// much of the whole is on screen, which a percentage never did.
func WithScrollBar(view string, h, total, visible, offset int) string {
	bar := ScrollBar(h, total, visible, offset)
	if bar == nil {
		return view
	}
	// A blank gutter column keeps text off the bar: a long line running
	// into the thumb is hard to read and looks like a rendering fault.
	// Callers reserve two columns for this form (gutter + bar).
	view = lipgloss.JoinHorizontal(lipgloss.Top, view,
		strings.Repeat(" \n", h-1)+" ")
	// Two colors, not one: the track takes the blurred-border color so it
	// reads as part of the frame, while the thumb takes the accent — the
	// moving part is the only thing worth the eye.
	for i, cell := range bar {
		if cell == scrollThumb {
			bar[i] = theme.ScrollThumbStyle.Render(cell)
		} else {
			bar[i] = theme.FrameStyle.Render(cell)
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, view, strings.Join(bar, "\n"))
}

// BorderScrollBar paints a scrollbar into a rendered box's right border,
// costing no content column at all. lipgloss has no per-row border hook
// (Border.Right is one string for every row), so this works on the
// rendered box: each content row is truncated by its final cell and the
// thumb glyph is appended in its place. ansi.Truncate is escape-aware, so
// the box's own styling survives and the width is unchanged.
//
// firstRow is the box line the content starts on — 1 past the top border,
// plus whatever chrome (a title, a head strip and rule) the box carries
// above its viewport. Getting it wrong paints the thumb onto the wrong
// row, so each call site pins it with a test.
func BorderScrollBar(box string, firstRow, rows, total, visible, offset int) string {
	bar := ScrollBar(rows, total, visible, offset)
	if bar == nil || total <= visible {
		return box
	}
	lines := strings.Split(box, "\n")
	w := lipgloss.Width(box)
	for i, cell := range bar {
		row := firstRow + i
		if row < 0 || row >= len(lines) || cell != scrollThumb {
			continue
		}
		lines[row] = ansi.Truncate(lines[row], w-1, "") + theme.ScrollThumbStyle.Render(cell)
	}
	return strings.Join(lines, "\n")
}

func NiceCeil(v float64) float64 {
	if v <= 10 {
		return 10
	}
	mag := math.Pow(10, math.Floor(math.Log10(v)))
	for _, mult := range []float64{1, 2, 5} {
		if v <= mult*mag {
			return mult * mag
		}
	}
	return 10 * mag
}
