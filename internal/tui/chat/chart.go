// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// Tokens graph geometry, shared by the renderer (viewTokens) and the click
// mapper (tokenBarAt) so they stay in sync. Each record draws as a pair of
// columns — in beside out — with a gap between records.
const (
	tokenBarWidth  = 1                 // one column of the pair
	tokenPairWidth = 2 * tokenBarWidth // in + out, flush
	tokenBarGap    = 1                 // between records
)

// tokenBarAt maps a screen click to the record of the Tokens graph pair under
// it (pairs are tokenPairWidth wide, tokenBarGap apart, from tokenBarX0;
// clicks on the gap or past the last pair miss).
func (m *Model) tokenBarAt(x, y int) (int, bool) {
	if m.tokensBlock() == 0 || len(m.tokenBarRecs) == 0 {
		return 0, false
	}
	chartH := m.tokensBlock() - 4
	top := m.rightTop() + 3 // pane border + title row + blank row
	if y < top || y >= top+chartH {
		return 0, false
	}
	dx := x - m.tokenBarX0
	pitch := tokenPairWidth + tokenBarGap
	if dx < 0 || dx%pitch >= tokenPairWidth { // in the gap between pairs
		return 0, false
	}
	if i := dx / pitch; i < len(m.tokenBarRecs) {
		return m.tokenBarRecs[i], true
	}
	return 0, false
}

// tokensLegend names the bar segments, each in its color; the full bar height
// is the record's total. The arrows match the wait status: ↑ what went up with
// the request, ↓ what came back.
func tokensLegend() string {
	return theme.TokensInStyle.Render("↑ in") + theme.DimStyle.Render(" · ") + theme.TokensOutStyle.Render("↓ out")
}

// tokenBar is one record's pair of columns: whole-cell heights for in and out.
type tokenBar struct{ in, out int }

// drawTokenBars renders the column pairs as chartH text rows (row 0 is the
// top): per record, an in (blue) column flush beside an out (purple) column —
// each measured against the same scale — with tokenBarGap spaces between
// records. Cell colors ride a Blend1D ramp per column, bright at the tip
// (where the value reads) and darkening toward the foot. Hand-rolled since
// the charm.land v2 migration (ntcharts is a v1 library).
func drawTokenBars(bars []tokenBar, chartH int) string {
	cell := strings.Repeat("█", tokenBarWidth)
	blank := strings.Repeat(" ", tokenBarWidth)
	gap := strings.Repeat(" ", tokenBarGap)

	// The legend keeps the base colors; a column's tip shows them in full.
	steps := max(2, chartH)
	inBase := theme.TokensInStyle.GetForeground()
	outBase := theme.TokensOutStyle.GetForeground()
	inRamp := lipgloss.Blend1D(steps, inBase, lipgloss.Darken(inBase, 0.35))
	outRamp := lipgloss.Blend1D(steps, outBase, lipgloss.Darken(outBase, 0.35))
	column := func(ramp []color.Color, b, height int) string {
		if b >= height {
			return blank
		}
		c := ramp[0] // single cell: the base color
		if height > 1 {
			c = ramp[(height-1-b)*(steps-1)/(height-1)]
		}
		return lipgloss.NewStyle().Foreground(c).Render(cell)
	}

	rows := make([]string, chartH)
	for r := range rows {
		cols := make([]string, len(bars))
		b := chartH - 1 - r // cell index counted from the bottom
		for i, bar := range bars {
			cols[i] = column(inRamp, b, bar.in) + column(outRamp, b, bar.out)
		}
		rows[r] = strings.Join(cols, gap)
	}
	return strings.Join(rows, "\n")
}

// viewTokens renders the token-usage graph pane: one in|out column pair per
// store record, newest on the left, derived from the store on every render.
func (m *Model) viewTokens() string {
	w := max(1, m.metricsWidth()-2)
	var used []*provider.Usage
	var recIDs []int
	for _, rec := range m.state.Records() {
		if rec.Usage != nil {
			used = append(used, rec.Usage)
			recIDs = append(recIDs, rec.ID)
		}
	}
	// Columns are per-component now, so the scale tops out at the largest
	// single side (usually the prompt), not the total.
	peak := 0.0
	for _, u := range used {
		peak = math.Max(peak, math.Max(float64(u.Prompt), float64(u.Completion)))
	}
	scale := render.NiceCeil(peak)

	// The scale is dim numbers in a column of their own left of the graph: the
	// rounded top value on the first chart row, its half at mid-height —
	// whole units both, in a column sized to the wider of the two (the half
	// can be the longer label: 1k over 500) so neither ever wraps.
	num := render.TokensAxis(int(scale))
	half := render.TokensAxis(int(scale) / 2)
	labelW := max(len(num), len(half))
	chartH := m.tokensBlock() - 4 // border + title/legend row + blank row
	chartW := max(1, w-labelW-1)
	// Newest record first: bars draw left to right, capped at what fits the
	// pane. The bar→record mapping and origin are cached for click-to-select
	// (tokenBarAt).
	m.tokenBarRecs = m.tokenBarRecs[:0]
	m.tokenBarX0 = m.chatColWidth() + 1 + labelW + 1
	// Quantize each column to whole cells so blocks are solid (no ragged
	// partial runes); a nonzero side always gets at least one cell.
	cellTokens := scale / float64(chartH)
	cells := func(v int) int {
		if v <= 0 {
			return 0
		}
		return min(chartH, max(1, int(math.Round(float64(v)/cellTokens))))
	}
	fit := (chartW + tokenBarGap) / (tokenPairWidth + tokenBarGap)
	var bars []tokenBar
	for i := len(used) - 1; i >= 0 && len(used)-1-i < fit; i-- {
		u := used[i]
		bars = append(bars, tokenBar{in: cells(u.Prompt), out: cells(u.Completion)})
		m.tokenBarRecs = append(m.tokenBarRecs, recIDs[i])
	}
	labels := make([]string, chartH)
	labels[0] = theme.DimStyle.Render(fmt.Sprintf("%*s", labelW, num))
	if chartH > 2 {
		labels[chartH/2] = theme.DimStyle.Render(fmt.Sprintf("%*s", labelW, half))
	}
	graph := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(labelW+1).Render(strings.Join(labels, "\n")),
		drawTokenBars(bars, chartH),
	)

	// Title row: " Tokens" left, the legend docked right on the same line
	// (truncated to the leftover width — "total" survives the longest).
	title := theme.PanelTitleStyle.Render(" Tokens")
	if avail := w - lipgloss.Width(title) - 3; avail > 0 {
		legend := ansi.Truncate(tokensLegend(), avail, "…")
		title += strings.Repeat(" ", w-lipgloss.Width(title)-lipgloss.Width(legend)-1) + legend
	}
	// The pane sits flush under the header; the empty string is the blank row
	// under the title, counted inside tokensBlock().
	return theme.BlurredBorder.Render(lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		lipgloss.NewStyle().Width(w).Render(graph),
	))
}
