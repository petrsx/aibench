// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package inspector

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/store"
	"github.com/petrsx/aibench/internal/tui/render"
)

// The request size breakdown: total sent bytes plus per-message rows
// with the largest marked — which part of a request blew, or is about
// to blow, a gateway's budget. A self-contained analyzer over one
// record.

// itemLabel names one entry of the conversation being sent: its `role`,
// else its `type`. Those are the only two names an item ever has — a
// message says what it is with a role, and the responses api's other
// input items are typed instead — and both are printed exactly as the
// body spells them. A prettier word of our own would be a name the
// reader cannot find when they go looking in the dump beside it.
func itemLabel(raw json.RawMessage) string {
	var m struct {
		Role string `json:"role"`
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &m)
	switch {
	case m.Role != "":
		return m.Role
	case m.Type != "":
		return m.Type
	}
	return "?"
}

// compactLen is a raw JSON value's size on the wire — the captured body
// may be pretty-printed, and indentation is not what was sent.
func compactLen(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return len(raw)
	}
	return buf.Len()
}

// requestBreakdown derives per-message sizes from the captured request body —
// which part of a request blew a gateway's budget (e.g. a content-safety
// policy failing closed on size), or is about to. Sizes are of the compact
// wire form, since the captured body may be pretty-printed. Rendered as a
// side-panel section under the headers, in the Metrics panel's shape: a
// title, then indented key/value rows.
func requestBreakdown(rec store.Record, width int) (string, []string) {
	// The api that made this request says where its body keeps things —
	// the record carries its own api, so a body captured before a profile
	// switch is still read by the rules that produced it.
	shape := provider.SpecFor(rec.Req.API).Body

	var body map[string]json.RawMessage
	if json.Unmarshal([]byte(rec.Req.ReqBody), &body) != nil {
		return "", nil
	}
	var items []json.RawMessage
	if json.Unmarshal(body[shape.Items], &items) != nil || len(items) == 0 {
		return "", nil
	}

	labels := make([]string, len(items))
	sizes := make([]int, len(items))
	total := 0
	for i, raw := range items {
		labels[i] = itemLabel(raw)
		sizes[i] = compactLen(raw)
		total += sizes[i]
	}

	// The parts that are not the conversation, named as the api names
	// them so a row and the body beside it read the same.
	type part struct {
		name string
		size int
	}
	var parts []part
	for _, name := range shape.Parts {
		if size := compactLen(body[name]); size > 0 {
			parts = append(parts, part{name, size})
			total += size
		}
	}

	sent := rec.Req.ReqBytes
	if sent == 0 {
		sent = int64(total)
	}

	// The rows account for the whole request: envelope + every part +
	// every item = the body total in the title. "envelope" is what went up
	// besides all of that — model, sampling params, stream flags — and
	// naming it is the point: a request can be mostly overhead, and the
	// earlier "body vs turns" pair left the reader to work that out.
	//
	// The table itself is the panels' shared one (render.KeyValues): the
	// api's longest name floors the key column so the numbers hold still
	// while the reader walks records, and the counts are right-aligned
	// because that is how a column of numbers is compared.
	// "2823 B", not "2823B": the unit is not a digit, and a column of
	// numbers is read down the digits.
	value := func(size int) string { return fmt.Sprintf("%d B", size) }
	rows := make([]render.KeyValue, 0, len(items)+len(parts)+1)
	if env := sent - int64(total); env > 0 {
		rows = append(rows, render.KeyValue{Key: "envelope", Value: value(int(env))})
	}
	for _, p := range parts {
		rows = append(rows, render.KeyValue{Key: p.name, Value: value(p.size)})
	}
	for i := range items {
		rows = append(rows, render.KeyValue{Key: labels[i], Value: value(sizes[i])})
	}
	lines := render.KeyValues(rows, width, render.KeyValueOpts{
		KeyFloor: shape.LabelWidth,
		// Enough for an ordinary body's byte count, so a record of small
		// parts and one of large parts draw the same table.
		ValueFloor: len("99999 B"),
		Right:      true,
	})

	// The header carries both totals, so the pane answers "how big, of
	// what" before anything is scrolled. "msg" is an abbreviation and
	// takes no plural; "item" is a word and does.
	unit := shape.Unit
	if unit == "item" && len(items) != 1 {
		unit = "items"
	}
	return fmt.Sprintf(" Request · %d %s · %s", len(items), unit, render.Bytes(sent)), lines
}
