// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

// How a transcript row is written. renderChat (transcript.go) walks the turns
// and assembles the maps; this file is what a row *is* — how a block wraps
// under its marker, which group claims it, and what one tool call lays out
// as. Both live behind builder, so the line list and the spans that index it
// can only move together.

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/store"
	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// builder collects the transcript as it is written: the lines, the groups
// that claim them, and the click targets found along the way.
type builder struct {
	m     *Model
	width int // content width, inside the selection gutter

	lines  []string
	groups []requestGroup
	open   int // index into groups, -1 when no group is open

	roundRows   []groupSpan // rows naming their own record (the tool rounds)
	resultSpans []groupSpan // the collapsible tool payloads

	// Tool results are rendered grouped under the ● call that requested them,
	// not where their RoleTool turn sits: they are indexed by call id up
	// front, and the ones a group consumed are marked so the main loop skips
	// their turn.
	results  map[string]store.Turn
	consumed map[int]bool

	shownID int    // the record whose chip lights up: the one Metrics shows
	dot     string // the tool call's marker, styled once per render
}

// newBuilder prepares a pass over turns at the given content width.
func newBuilder(m *Model, width int, turns []store.Turn) *builder {
	b := &builder{
		m: m, width: width, open: -1,
		results:  make(map[string]store.Turn),
		consumed: make(map[int]bool),
		shownID:  -1,
		dot:      theme.DimStyle.Render("● "),
	}
	for _, t := range turns {
		if t.Role == provider.RoleTool && t.ToolCallID != "" {
			b.results[t.ToolCallID] = t
		}
	}
	if rec, ok := m.state.ShownRecord(); ok {
		b.shownID = rec.ID
	}
	return b
}

// emit is the one way a row reaches the transcript: it appends the line and
// hands it to the open group, so ownership is recorded where the line is
// written rather than reconstructed from spans afterwards.
func (b *builder) emit(l string) {
	b.lines = append(b.lines, l)
	if b.open < 0 {
		return
	}
	g := &b.groups[b.open]
	g.end = len(b.lines)
	if strings.TrimSpace(ansi.Strip(l)) != "" {
		g.text = len(b.lines) - 1
	}
}

// openGroup / closeGroup bracket a group around the rows one turn produces.
func (b *builder) openGroup(recordID int) {
	b.groups = append(b.groups, requestGroup{
		recordID: recordID, start: len(b.lines), end: len(b.lines), text: -1,
	})
	b.open = len(b.groups) - 1
}

func (b *builder) closeGroup() { b.open = -1 }

// block and blockLines both size the wrap and the continuation hang from the
// marker's own width, so wide markers (the tool results' "  ⎿  ") indent
// cleanly.
func (b *builder) block(first string, style *lipgloss.Style, text string) {
	pw := lipgloss.Width(first)
	wrap := lipgloss.NewStyle().Width(max(1, b.width-pw))
	for i, l := range strings.Split(wrap.Render(text), "\n") {
		prefix := strings.Repeat(" ", pw) // continuation lines hang under the marker
		if i == 0 {
			prefix = first
		}
		if style != nil {
			l = style.Render(l)
		}
		b.emit(prefix + l)
	}
}

// blockLines emits already-rendered content (e.g. colorized JSON) under the
// marker; each line still wraps (ANSI-aware) to the chat width.
func (b *builder) blockLines(first string, rendered []string) {
	pw := lipgloss.Width(first)
	lineWrap := lipgloss.NewStyle().Width(max(1, b.width-pw-2))
	prefix := first
	for _, l := range rendered {
		for sub := range strings.SplitSeq(lineWrap.Render(l), "\n") {
			b.emit(prefix + sub)
			prefix = strings.Repeat(" ", pw)
		}
	}
}

// recordRow emits a turn's record line. The open group claims it like every
// other row, but the row also names its own record: inside an expanded tool
// call the rounds are separate requests, and clicking a round's line must
// select that round, not the exchange around it.
func (b *builder) recordRow(rec store.Record, total time.Duration) {
	if b.m.hideDiag { // the conversation, without what it cost
		return
	}
	if w := b.m.recordLineFor(rec, total); w != "" {
		b.emit(render.RecordRow(w, b.width))
		b.roundRows = append(b.roundRows, groupSpan{
			start: len(b.lines) - 1, end: len(b.lines), recordID: rec.ID,
		})
	}
}

// toolGroup renders one requested call: the dim ● line naming it, the round's
// record row, and the result beneath. None of that folds away — a call the
// model made and the request it cost are what happened, and hiding them
// behind a toggle only made you click to learn it. The payload itself does
// fold: a long result stays behind its own ⎿ header (resultSpans), so a big
// body never lands uninvited.
func (b *builder) toolGroup(c provider.ToolCall) {
	label := c.Name + "(" + compactArgs(c.Arguments) + ")"
	res, ok := b.results[c.ID]
	if !ok { // unbound, or the run is still out: just the call line
		b.block(b.dot, &theme.DimStyle, label)
		return
	}
	b.consumed[res.ID] = true
	// The call line heads the round, so it belongs to it — with its request
	// and what that request brought back. The reply that asked for the call
	// was produced by the *previous* request, and owning the line by that
	// reading put it in the previous round's block: two rows of rule past
	// where the block looks like it ends, and a click on the call line
	// selecting the round before it.
	roundStart := len(b.lines)
	b.block(b.dot, &theme.DimStyle, label)
	defer func() {
		if res.RecordID >= 0 && len(b.lines) > roundStart {
			b.roundRows = append(b.roundRows, groupSpan{
				start: roundStart, end: len(b.lines), recordID: res.RecordID,
			})
		}
	}()
	// The round's follow-up request sits above the result it carried.
	if rec, ok := b.m.state.RecordByID(res.RecordID); ok && res.RecordID >= 0 {
		b.recordRow(rec, res.Total)
	}
	marker := theme.DimStyle.Render("  ⎿  ")
	if len(res.Content) <= toolInlineLimit && !strings.Contains(res.Content, "\n") {
		b.block(marker, &theme.DimStyle, res.ToolName+" → "+res.Content)
		return
	}
	// The payload is the one thing left that folds, so its header is the one
	// place the instruction is worth its width: with nothing else clickable in
	// the group, a bare glyph would be the only hint that the body is there at
	// all.
	rhead := len(b.lines)
	glyph, hint := "▸", hintExpand
	if b.m.resultOpen[res.ID] {
		glyph, hint = "▾", hintCollapse
	}
	b.block(marker, &theme.DimStyle,
		fmt.Sprintf("%s %s %s · %s", res.ToolName, glyph, b.m.payloadSummary(res), hint))
	b.resultSpans = append(b.resultSpans, groupSpan{start: rhead, end: len(b.lines), recordID: res.ID})
	if !b.m.resultOpen[res.ID] {
		return
	}
	if b.m.isJSONBody(res) {
		b.blockLines("     ", b.m.prettyBody(res))
	} else {
		b.block("     ", &theme.DimStyle, res.Content)
	}
}

// turn lays out everything one turn produces, bracketed as one group. A
// record owns its whole exchange, not just its summary line: the question
// that triggered it and the reply it produced are what the Metrics panel
// measures, so clicking any of those rows selects it. Anything narrower makes
// the user hunt for a one-row target. Turns with no record of their own (a
// notice, an interrupt) open a group anyway so their rows are still
// bracketed, just owned by nothing.
func (b *builder) turn(t store.Turn) {
	b.openGroup(t.RecordID)
	if t.Role == provider.RoleTool && b.consumed[t.ID] {
		// Already rendered inside its ● call's group above. The group is
		// left open on purpose: it wrote no rows of its own, so anything
		// trailing the loop — the wait status of the round this result
		// belongs to — is claimed by the round rather than by nothing.
		return
	}
	defer b.closeGroup()

	switch t.Role {
	case provider.RoleUser:
		b.block(theme.UserRowStyle.Render("> "), &theme.UserRowStyle, t.Content)
	case provider.RoleAssistant:
		b.assistant(t)
	case provider.RoleTool:
		// A result with no matching call (defensive): a dim ⎿ block, indented
		// like the record row.
		b.block(theme.DimStyle.Render("  ⎿  "), &theme.DimStyle, t.ToolName+" → "+t.Content)
	case store.RoleError:
		b.block(theme.ErrorStyle.Render("⚠ "), &theme.ErrorStyle, t.Content)
	case store.RoleNotice:
		b.block(theme.WarningStyle.Render("⚠ "), &theme.WarningStyle, t.Content)
	default:
		b.block(theme.DimStyle.Render(t.Role+" "), &theme.DimStyle, t.Content)
	}
	b.trailingRecord(t)
	// The separator closes the block from inside, so it belongs to this
	// group: a click there selects the exchange it trails.
	b.emit("")
}

// assistant lays out a reply: its text, then the tool calls it requested —
// args verbatim, each with its result (and round record) inside. A completed
// reply that is pure JSON renders with the same token colors as the
// Inspector; prose keeps the wrapped block.
func (b *builder) assistant(t store.Turn) {
	// ● (U+25CF, Geometric Shapes) rather than ⏺ (U+23FA, Misc
	// Technical): Cascadia Mono carries the first and not the second, so
	// Windows Terminal substituted an emoji font for the record glyph and
	// drew a blue box. A terminal app cannot pick the font — only
	// characters the fonts people run already have.
	first := theme.AssistantStyle.Render("● ")
	if t.Status == store.InProgress {
		first = b.m.prog.Spinner + " "
	}
	switch {
	case t.Content == "" && len(t.ToolCalls) > 0:
		// A tool-only reply has no text — just the calls below.
	case t.Status == store.Complete && b.m.isJSONBody(t):
		b.blockLines(first, b.m.prettyBody(t))
	default:
		b.block(first, nil, t.Content)
	}
	for _, c := range t.ToolCalls {
		b.toolGroup(c)
	}
}

// trailingRecord emits the turn's record line, where it has one. Only that
// line is a pin target — the message text is just text. It hangs off the user
// turn that triggered the request (tool rounds render theirs inside the call's
// group) as a single row, derived fresh, with a right-aligned metrics chip
// carrying the selection state.
func (b *builder) trailingRecord(t store.Turn) {
	rec, ok := b.m.state.RecordByID(t.RecordID)
	if ok && t.RecordID >= 0 && (t.Role == provider.RoleUser || t.Role == provider.RoleTool) {
		// Turn.Total spans every round of the send, so on a question that
		// took a tool loop it says what the Exchange group says better (with
		// the request count that explains it). Pass it only when this
		// question was one request, where it can still mean the one thing the
		// record line has no other way to show: wall-clock lost to 429
		// backoff.
		total := t.Total
		if len(b.m.exchangeOf(t.RecordID)) > 1 {
			total = 0
		}
		b.recordRow(rec, total)
		return
	}
	if t.ID == b.m.prog.PendingTurn && t.Role == provider.RoleUser {
		// The request is in flight but no record exists yet (before TTFB):
		// reserve the record row so the spinner below doesn't jump down.
		b.emit(theme.DimStyle.Render("  ⎿  waiting for response…"))
	}
}

// waitStatus names the wait while a send is out before its first delta: the
// spinner, the elapsed time and how much prompt went up (tokens estimated
// from the payload — the real count only arrives with the final usage). While
// a tool round's runs are still out, the wait is theirs, not the model's.
func (b *builder) waitStatus() {
	if !b.m.prog.Streaming || b.m.prog.StreamTurn >= 0 {
		return
	}
	status := fmt.Sprintf("thinking… (%ds · ↑ ~%d tokens)",
		int(time.Since(b.m.prog.SendAt).Seconds()), max(1, b.m.prog.SentChars/4))
	if b.m.prog.PendingTools > 0 {
		status = fmt.Sprintf("running tools… (%d outstanding)", b.m.prog.PendingTools)
	}
	b.block(b.m.prog.Spinner+" ", &theme.DimStyle, status)
}

// queuedMessages shows what was typed while the send was in flight, in the
// order it will go up. They are shown as dim user turns rather than counted in
// a notice: five queued messages are five things you wrote, and a single
// "queued (5)" line neither says which nor lets you read them back. Dim, and
// marked, because they have not been sent — the transcript must never imply
// something went on the wire that did not.
func (b *builder) queuedMessages() {
	for i, q := range b.m.queued {
		b.emit("")
		b.block(theme.DimStyle.Render("> "), &theme.DimStyle, q)
		b.emit(theme.DimStyle.Render(
			fmt.Sprintf("  ⎿  queued · %d of %d", i+1, len(b.m.queued))))
	}
}

// spans is the record map the mouse reads: every row a turn produced,
// separator included, points back at the exchange that wrote it. Tool calls
// and their results are sub-targets inside those groups, spans of the same
// kind — a click resolves innermost-first (see handleMouse). The round rows go
// last: expandSpans lets a later span win, so a row that names its own record
// beats the group it sits inside.
func (b *builder) spans() []groupSpan {
	out := make([]groupSpan, 0, len(b.groups)+len(b.roundRows))
	for _, g := range b.groups {
		if g.recordID >= 0 {
			out = append(out, g.span())
		}
	}
	return append(out, b.roundRows...)
}
