// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

// The transcript pane: the Claude Code-style message log with per-turn record
// lines, re-derived from the store each refresh and cached for mouse selection.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/store"
	"github.com/petrsx/aibench/internal/tui/find"
	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/selection"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// The payload control's words. They are the hit target — a row is text you
// may want to select, and only the part that says it does something does
// anything (see resultHits).
const (
	hintExpand   = "click to expand"
	hintCollapse = "click to collapse"
)

// hitSpan is a control's cell span on one line of the transcript.
type hitSpan struct {
	line       int
	col0, col1 int
	id         int
}

// resultHits locates the payload controls in the finished lines, so a
// click is tested against the words as drawn — gutter, wrapping and all.
func resultHits(lines []string, lineResult []int) []hitSpan {
	var out []hitSpan
	for i, id := range lineResult {
		if id < 0 || i >= len(lines) {
			continue
		}
		plain := ansi.Strip(lines[i])
		for _, hint := range []string{hintExpand, hintCollapse} {
			at := strings.Index(plain, hint)
			if at < 0 {
				continue
			}
			col := ansi.StringWidth(plain[:at])
			out = append(out, hitSpan{
				line: i, col0: col, col1: col + ansi.StringWidth(hint), id: id,
			})
			break
		}
	}
	return out
}

// groupSpan is a run of chatLines rows that belongs to one thing: a
// request group (below), a tool call's collapsible block, or a tool
// result's payload. The line maps derived from these are what mouse
// hit-testing reads.
type groupSpan struct {
	start, end int // [start, end) in chatLines
	recordID   int
}

// requestGroup is one request's block of the transcript: the question that
// triggered it, the reply it produced, the tool rounds inside that reply,
// its record row — and the blank row that closes the block.
//
// Lines are claimed as they are emitted, so the blank rows are not a case
// to reason about afterwards: a separator inside a block belongs to that
// block because that is where it was written. text is where the block's
// last line of text sits, which is where the selection rule stops — the
// closing blank is part of the group (a click there selects it) but
// painting it would hang the rule below the block it marks.
type requestGroup struct {
	recordID   int
	start, end int // [start, end) in chatLines, closing blank included
	text       int // last row with text in it; -1 while the group has none
}

// span is the group as a click target.
func (g requestGroup) span() groupSpan {
	return groupSpan{start: g.start, end: g.end, recordID: g.recordID}
}

// The selection gutter: the leftmost columns of every transcript line, where
// the shown record's whole group — the question, the reply, the record line —
// wears a rule. A record is an exchange, not a row, so the mark has to have
// the shape of one; the banded record line it replaces could only ever
// highlight the summary. The glyph is the composer's own prompt rule, so
// "this block" reads the same in the transcript as it does in the input.
const (
	gutterWidth  = 2
	gutterRule   = "┃" // the request the panels are showing
	exchangeRule = "│" // the rest of the exchange it belongs to
)

// markGroup prefixes every line with the gutter and rules the rows of the
// shown exchange — every record it took to answer one question, from the
// question's first row through the last row of text in the answer. The
// blocks in between and the separators joining them come along, which is
// what makes one question read as one thing however many requests it cost.
// It stops at the last row with text: the closing blank still belongs to
// the exchange for a click, but painting it would hang the rule below the
// block.
//
// Two weights share the column: the light rule spans the exchange, the
// heavy one covers the rows of the single request the panels are showing.
// One question can be several requests, and clicking a round switches what
// Metrics measures — without the second weight the mark could not say
// which of them you had picked.
//
// Widths are unchanged: the callers below laid their content out inside
// the remaining width.
func markGroup(lines []string, groups []requestGroup, shown map[int]bool, owner []int, shownID int) []string {
	blank := strings.Repeat(" ", gutterWidth)
	pad := strings.Repeat(" ", gutterWidth-1)
	heavy := theme.SelectionRuleStyle.Render(gutterRule) + pad
	light := theme.ExchangeRuleStyle.Render(exchangeRule) + pad
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = blank + l
	}
	if len(shown) == 0 {
		return out
	}
	first, last := -1, -1
	for _, g := range groups {
		if !shown[g.recordID] || g.text < 0 {
			continue
		}
		if first < 0 {
			first = g.start
		}
		last = g.text
	}
	// Weigh the rows that have something on them, then the separators
	// between them: a blank is heavy only where both the block above and
	// the one below are, so an interior gap stays inside its block and a
	// boundary gap belongs to neither. Weighing blanks by the group that
	// wrote them — the exchange's first request — striped one block like
	// several; weighing them by the row above alone hung the rule two rows
	// past where the block ends.
	heavyRow := make([]bool, len(out))
	hasText := make([]bool, len(out))
	for i := first; i >= 0 && i <= last && i < len(out); i++ {
		hasText[i] = strings.TrimSpace(ansi.Strip(lines[i])) != ""
		heavyRow[i] = hasText[i] && i < len(owner) && owner[i] == shownID
	}
	for i := first; i >= 0 && i <= last && i < len(out); i++ {
		on := heavyRow[i]
		if !hasText[i] {
			on = nearestHeavy(heavyRow, hasText, i, -1) && nearestHeavy(heavyRow, hasText, i, 1)
		}
		rule := light
		if on {
			rule = heavy
		}
		out[i] = rule + lines[i]
	}
	return out
}

// nearestHeavy reports whether the closest row with text in the given
// direction is part of the shown request's block.
func nearestHeavy(heavyRow, hasText []bool, from, step int) bool {
	for i := from + step; i >= 0 && i < len(hasText); i += step {
		if hasText[i] {
			return heavyRow[i]
		}
	}
	return false
}

// renderChat rebuilds the chat viewport in a Claude Code-like layout: user
// messages as dim "> " blocks, assistant replies bulleted. The resulting lines
// are cached for mouse selection; follow keeps the view pinned to the bottom
// (skipped while a selection is being dragged).
func (m *Model) renderChat(follow bool) {
	// Every line is laid out inside the selection gutter (see markGroup), so
	// the content width is the viewport less that column.
	turns := m.state.Turns()
	m.forgetDerived(turns)
	b := newBuilder(m, max(1, m.chatView.Width()-gutterWidth), turns)

	// No welcome banner for starters: launch already opens the /starters
	// selection when a set declares them, so the empty transcript stays clean.
	for _, t := range turns {
		b.turn(t)
	}
	b.waitStatus()
	b.queuedMessages()

	m.lineRec = expandSpans(b.spans(), len(b.lines))
	m.lineResult = expandSpans(b.resultSpans, len(b.lines))
	// The mark spans the whole exchange the shown record belongs to: the
	// question, its tool rounds, and the answer are one thing to the person
	// who asked, however many requests they took.
	shown := map[int]bool{}
	for _, rec := range m.exchangeOf(b.shownID) {
		shown[rec.ID] = true
	}
	if len(shown) == 0 && b.shownID >= 0 {
		shown[b.shownID] = true // a record no turn claims: mark it alone
	}
	m.chatLines = markGroup(b.lines, b.groups, shown, m.lineRec, b.shownID)
	m.resultHits = resultHits(m.chatLines, m.lineResult)

	out := m.chatLines
	if m.sel.Pane == paneChat {
		out = selection.Apply(m.chatLines, m.sel.Anchor, m.sel.Head)
	}
	// A live search re-derives its matches over the fresh transcript and bakes
	// the highlight styles into the shown lines (the viewport's own highlight
	// API mis-attributes lines on styled content — see the find package). The
	// view is not scrolled here: only a query change or next/prev walks it.
	if m.find.Active() {
		m.find.Refresh(m)
		out = find.Highlight(out, m.find.Hits(), m.find.Current())
	}
	m.chatView.SetContentLines(out)
	// A finished (kept) selection doesn't pin the view — only an active drag.
	// An open search holds the view on its current match instead of following.
	// stickBottom is the user's say: while a reply streams this render runs
	// every frame, so following unconditionally would yank the view back down
	// ten times a second and make scrolling up impossible.
	if follow && m.stickBottom && !m.find.Active() && (!m.sel.Drag || m.sel.Pane != paneChat) {
		m.chatView.GotoBottom()
	}
}

// recordLineFor derives a turn's record line: the request
// summary, then tokens, then the request id. Nothing is baked at event time,
// so resizes and late data re-derive cleanly.
func (m *Model) recordLineFor(rec store.Record, total time.Duration) string {
	if rec.Req.Method == "" {
		return ""
	}
	line, _ := render.RequestSummary(m.spec, rec.Req)
	if rec.Usage != nil {
		line += "  " + render.RenderUsage(rec.Usage)
	}
	if id, ok := m.spec.LogMeta[0].Pick(rec.Req.RespHeaders); ok {
		line += "  " + render.RenderMeta([]capture.KeyValue{id})
	}
	// The line shows the winning request's own latency (reqDur: the full
	// request time, TTFB as a fallback). A send whose wall-clock exceeds
	// that spent the difference on 429 backoff, which nothing else reveals,
	// so it is named here. The caller passes 0 when a tool loop is what
	// made the two diverge instead: the Exchange group reports that, with
	// the request count that explains it, where a bare "total" only invited
	// the question.
	reqDur := rec.Req.Duration
	if rec.Body.Duration > 0 {
		reqDur = rec.Body.Duration
	}
	if total-reqDur >= 500*time.Millisecond {
		line += "  " + theme.DimStyle.Render("total "+render.Elapsed(total))
	}
	return line
}

// toolInlineLimit is the longest single-line tool result shown inline;
// anything bigger collapses behind its summary header.
const toolInlineLimit = 80

// toolSummary sizes up a collapsed tool result: item count for JSON
// arrays, byte size for everything.
func toolSummary(s string) string {
	size := render.Bytes(int64(len(s)))
	var items []json.RawMessage
	if json.Unmarshal([]byte(s), &items) == nil {
		return fmt.Sprintf("%d items · %s", len(items), size)
	}
	return size
}

// compactArgs tidies a tool call's raw JSON arguments for the one-line
// call rendering: JSON whitespace compacted, an empty object elided.
func compactArgs(raw string) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(raw)); err == nil {
		raw = buf.String()
	} else {
		raw = strings.TrimSpace(raw)
	}
	if raw == "{}" || raw == "null" {
		return ""
	}
	return raw
}

// expandSpans turns record line ranges into a per-line record-id lookup of
// length n (-1 where no record owns the line).
func expandSpans(spans []groupSpan, n int) []int {
	rec := make([]int, n)
	for i := range rec {
		rec[i] = -1
	}
	for _, s := range spans {
		for i := s.start; i < s.end && i < n; i++ {
			rec[i] = s.recordID
		}
	}
	return rec
}
