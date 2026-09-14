// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package inspector

import (
	"fmt"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/store"
	"github.com/petrsx/aibench/internal/tui/find"
	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/selection"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// Content derivation: the shown record's capture data turned into the
// viewport lines each sub-view shows — request, response, and the
// headers panel. Pure store → lines, re-derived by Refresh on version
// change.

// Refresh re-derives the dump and headers from the store: the active
// sub-view's body into the dump, its headers (plus the assembled message on
// the response) into the side panel. Scroll jumps to the top when the shown
// record or sub-view changes, and holds while it only grows.
func (m *Model) Refresh() {
	var body, headerSrc, sizeSrc []string
	shownID := -1
	if rec, ok := m.state.ShownRecord(); ok {
		shownID = rec.ID
		body = m.rawLinesFor(rec)
		headerSrc = m.headerLinesFor(rec)
		// Request-only: the breakdown describes what went up. Its title
		// carries the message count, so a long list reads at a glance
		// without scrolling the pane.
		if m.view == viewRequest {
			// Inset adds the pane's one-column pad, so the rows are built
			// one cell narrower than the pane itself.
			m.sizesTitle, sizeSrc = requestBreakdown(rec, m.sizes.Width()-1)
		}
	}
	// Content decides the split, so it is settled before the layout: the
	// sub-view or the record may have changed since the last pass.
	m.hasSizes = len(sizeSrc) > 0
	m.layoutRight()

	// One column of left padding lives inside the cached lines so mouse
	// selection maps to what is shown.
	// The dump is main-column content: no pad of its own, the column pads.
	// WrapIndent, so a body too wide for the pane keeps its nesting; the
	// spans are read off the wrapped lines, since a click lands on what is
	// drawn rather than on the text before it was folded.
	m.dumpLines = render.WrapIndent(body, m.dump.Width())
	if m.find.Active() {
		m.find.Refresh(m) // fresh content: recompute the matches
	}
	m.refreshDump()

	m.headersLines = render.InsetClip(headerSrc, m.headers.Width())
	hout := m.headersLines
	if m.sel.Pane == paneHeaders {
		hout = selection.Apply(m.headersLines, m.sel.Anchor, m.sel.Head)
	}
	m.headers.SetContentLines(hout)

	m.sizesLines = render.Inset(sizeSrc, m.sizes.Width())
	sout := m.sizesLines
	if m.sel.Pane == paneSizes {
		sout = selection.Apply(m.sizesLines, m.sel.Anchor, m.sel.Head)
	}
	m.sizes.SetContentLines(sout)

	if m.sel.Drag && (m.sel.Pane == paneDump || m.sel.Pane == paneHeaders || m.sel.Pane == paneSizes) {
		return
	}
	if shownID != m.shownID || m.view != m.shownView {
		m.shownID, m.shownView = shownID, m.view
		m.dump.GotoTop()
		m.headers.GotoTop()
		m.sizes.GotoTop()
	}
}

// refreshDump re-derives the dump viewport from the cached lines: the
// selection overlay first, then the find highlights baked on top (both
// restyle in place, so the cell grid stays aligned with dumpLines).
func (m *Model) refreshDump() {
	out := m.dumpLines
	if m.sel.Pane == paneDump {
		out = selection.Apply(m.dumpLines, m.sel.Anchor, m.sel.Head)
	}
	if m.find.Active() {
		out = find.Highlight(out, m.find.Hits(), m.find.Current())
	}
	m.dump.SetContentLines(out)
}

// shownBody is the sub-view's body exactly as it was sent or received —
// what the copy control hands over, so it pastes as the bytes rather than
// as the reading aid this pane makes of them.
func (m *Model) shownBody() string {
	rec, ok := m.state.ShownRecord()
	if !ok {
		return ""
	}
	if m.view == viewRequest {
		return rec.Req.ReqBody
	}
	if rec.Final != "" {
		return rec.Final
	}
	return rec.Body.Body
}

// rawLinesFor renders one record's active sub-view; the record identity
// (time, method, path, status) lives in the record frame.
func (m *Model) rawLinesFor(rec store.Record) []string {
	if rec.Req.Method == "" { // transport failure: nothing was sent
		return []string{theme.ErrorStyle.Render(rec.Err)}
	}
	if m.view == viewRequest {
		return m.requestLines(rec)
	}
	return m.responseLines(rec)
}

// requestLines is the request sub-view: exactly what went up and nothing
// else — the sent body (where prompt engineering lands: system prompt,
// history, sampling params, tool results). The per-message size breakdown
// used to lead this pane; it is a summary *about* the body rather than the
// body, so it lives in the side panel now (see requestBreakdown) and the
// dump stays a faithful view of the bytes. A capture-capped body leads with
// a marker so a mid-JSON cut is never mistaken for what was actually sent.
func (m *Model) requestLines(rec store.Record) []string {
	if rec.Req.ReqBody == "" {
		return nil
	}
	var lines []string
	if rec.Req.ReqBytes > int64(len(rec.Req.ReqBody)) {
		lines = append(lines, theme.DimStyle.Render(
			fmt.Sprintf("kept first %dKB of %dB sent", capture.MaxBody>>10, rec.Req.ReqBytes)))
	}
	return append(lines, render.PrettyBodyWith(rec.Req.ReqBody, m.decoded)...)
}

// responseLines is the response sub-view: the assembled final body (the SDK's
// accumulation of the stream — the raw chunks are dropped to save memory), with
// the condensed error leading on failures. Error responses carry no assembled
// body, so they fall back to the raw captured payload. Request context stays
// in the request sub-view — the tabs keep their semantics.
func (m *Model) responseLines(rec store.Record) []string {
	var lines []string
	if rec.Err != "" {
		lines = append(lines, theme.ErrorStyle.Render(rec.Err))
	}
	if rec.Final != "" {
		return append(lines, render.PrettyBodyWith(rec.Final, m.decoded)...)
	}
	if rec.Body.Kind == capture.KindBody {
		if rec.Body.Truncated {
			lines = append(lines, theme.DimStyle.Render(
				fmt.Sprintf("kept first %dKB of %dB received", capture.MaxBody>>10, rec.Body.BodyBytes)))
		}
		lines = append(lines, render.PrettyBodyWith(rec.Body.Body, m.decoded)...)
	}
	return lines
}

// headerLinesFor renders the active sub-view's headers for the side panel:
// sent headers on the request view, received on the response. The assembled
// reply is no longer duplicated here — it is the response body now.
func (m *Model) headerLinesFor(rec store.Record) []string {
	headers := rec.Req.RespHeaders
	if m.view == viewRequest {
		headers = rec.Req.ReqHeaders
	}
	// Not a table: a header is a sentence — "content-type: text/event-
	// stream" — and column-aligning a list whose names run from "date" to
	// "x-ms-deployment-name" would spend half the pane on whitespace. The
	// row wraps and trims as one thing, which is how it is read.
	lines := make([]string, 0, len(headers))
	for _, kv := range headers {
		lines = append(lines, theme.MetaKeyStyle.Render(kv.Key+": ")+theme.DimStyle.Render(kv.Value))
	}
	return lines
}
