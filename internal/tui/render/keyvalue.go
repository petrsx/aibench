// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

// The side panels' one table. Metrics, Headers, Request sizes and Params
// are all the same shape — a name and what it is worth — and they had
// each grown their own arithmetic: one padded the key to the widest key,
// one pushed the value to the pane's far edge, one let the whole line
// wrap. Read side by side they looked like three tables.
//
// One rule, in one place: the names take a column, the values take
// theirs, and when the pane is too narrow for both the name is what
// gives. A name is guessable from its first half; a value cut in half is
// just wrong.

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// KeyValue is one row. Value is rendered as given — a caller that colours
// its values (the Tokens counts, a dim placeholder) keeps that colour;
// the key is styled here, so the panels cannot drift apart on it.
type KeyValue struct {
	Key   string
	Value string
	// KeyStyle overrides the key's own styling for rows that carry state
	// in the name — the Prompt tab's focused field, or a param this api
	// does not take. Zero value: the shared key style.
	KeyStyle lipgloss.Style
}

// KeyValueOpts tunes the table without moving the rule that shapes it.
type KeyValueOpts struct {
	// Indent is the cells before the key: the panel's own inset.
	Indent int
	// KeyFloor keeps the key column from shrinking below a width the
	// caller knows it will need again — so a table read by walking from
	// record to record does not slide sideways under the reader.
	KeyFloor int
	// KeyW fixes the key column outright, for a panel drawing several
	// tables that must line up with each other (the Metrics groups).
	KeyW int
	// ValueFloor is the same promise for the value column: a table of
	// small numbers and one of large numbers should still be one width.
	ValueFloor int
	// Right aligns values on their right edge, for columns of numbers.
	// Text reads better from the left.
	Right bool
}

// KeyColumn is the width the keys would take: the widest of them, floored
// by opts.KeyFloor and capped by what the pane can spare. A caller
// rendering several tables that must agree passes this back as KeyW.
func KeyColumn(rows []KeyValue, width int, opts KeyValueOpts) int {
	keyW := opts.KeyFloor
	valueW := opts.ValueFloor
	for _, r := range rows {
		keyW = max(keyW, lipgloss.Width(r.Key))
		valueW = max(valueW, lipgloss.Width(r.Value))
	}
	// The value column is never squeezed to nothing: past that point the
	// pane is too narrow for a table at all, and something has to give.
	return max(1, min(keyW, width-opts.Indent-kvGap-min(valueW, max(1, width/2))))
}

// kvGap is the daylight between the two columns — enough to read them as
// two, not so much that a row becomes a journey.
const kvGap = 2

// KeyValues renders the rows. Lines are exactly as wide as the table
// needs, never as wide as the pane: a value pushed out to the far edge
// drifts further from its name the wider the terminal gets.
func KeyValues(rows []KeyValue, width int, opts KeyValueOpts) []string {
	if len(rows) == 0 {
		return nil
	}
	keyW := opts.KeyW
	if keyW == 0 {
		keyW = KeyColumn(rows, width, opts)
	}
	valueW := opts.ValueFloor
	for _, r := range rows {
		valueW = max(valueW, lipgloss.Width(r.Value))
	}
	valueW = min(valueW, max(1, width-opts.Indent-kvGap-keyW))

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		key := ansi.Truncate(r.Key, keyW, "…")
		val := ansi.Truncate(r.Value, valueW, "…")
		pad := keyW - lipgloss.Width(key) + kvGap
		if opts.Right {
			pad += valueW - lipgloss.Width(val)
		}
		keyStyle := theme.MetaKeyStyle
		if r.KeyStyle.String() != (lipgloss.Style{}).String() {
			keyStyle = r.KeyStyle
		}
		out = append(out, strings.Repeat(" ", opts.Indent)+
			keyStyle.Render(key)+strings.Repeat(" ", pad)+val)
	}
	return out
}
