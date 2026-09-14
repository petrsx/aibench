// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package prompt

import (
	"log/slog"
	"slices"

	"charm.land/bubbles/v2/textinput"

	"github.com/petrsx/aibench/internal/bindings"
	"github.com/petrsx/aibench/internal/provider"
)

// The params pane: one row per param the prompt file actually defines,
// parsed back out for the shell's send, and the grid geometry that lays
// them out beside the rendered prompt.
//
// The rows follow the file, not the api. Listing every knob the api has
// filled the panel with empty boxes for things nobody set — five rows to
// say two — when the api's whole list is a reference question, answered
// by the docs. What the panel is for is the values in front of you.

// paramRow is one row: the name as the file spells it, and whether the
// active api offers it. A name it does not offer is still shown — the
// file says it, and a param silently ignored is how a typo survives a
// whole session — but it is marked, and it is never sent.
type paramRow struct {
	name  string
	known bool
}

// paramRows are the rows for a set of frontmatter params: the api's own,
// in the api's order, then anything else the file carries, sorted. A
// stable order matters because the panel is edited in place.
func (m *Model) paramRows(params map[string]string) []paramRow {
	var rows []paramRow
	seen := map[string]bool{}
	for _, def := range m.spec.Params {
		if _, set := params[def.Name]; set {
			rows = append(rows, paramRow{def.Name, true})
			seen[def.Name] = true
		}
	}
	var foreign []string
	for name := range params {
		if !seen[name] {
			foreign = append(foreign, name)
		}
	}
	slices.Sort(foreign)
	for _, name := range foreign {
		rows = append(rows, paramRow{name, false})
	}
	return rows
}

// buildParamInputs (re)creates the inputs for the current rows, hinted
// with the api's ranges where it knows the name.
func (m *Model) buildParamInputs() {
	m.params = make([]textinput.Model, len(m.rows))
	for i, row := range m.rows {
		ti := textinput.New()
		ti.Placeholder = m.paramHint(row.name)
		ti.Prompt = ""
		ti.SetWidth(10)
		ti.CharLimit = 10
		if row.name == "stop" { // comma-separated sequences need room
			ti.SetWidth(16)
			ti.CharLimit = 64
		}
		m.params[i] = ti
	}
	m.focus = 0
}

// paramHint is the api's range for a name, or the empty placeholder for
// one it does not offer.
func (m *Model) paramHint(name string) string {
	for _, def := range m.spec.Params {
		if def.Name == name {
			if def.Hint == "" {
				return "default"
			}
			return def.Hint
		}
	}
	return ""
}

// loadRequest (re)reads the set's declared tool files. An error keeps
// the previous set and logs — a mid-edit save must not silently strip
// the request's tools, and the CLI path reports the same error loudly.
func (m *Model) loadRequest() {
	set, err := bindings.LoadAll(m.toolsFiles)
	m.toolsErr = err
	if err != nil {
		slog.Warn("tools not reloaded", "err", err)
		return
	}
	m.request = set
}

// SystemPrompt is the raw markdown behind the view (sent as the system message).
func (m *Model) SystemPrompt() string { return m.sysSource }

// Request is the prompt's tools file: the per-api body overlays
// and tool bindings the shell bridges into each send.
func (m *Model) Request() bindings.Set { return m.request }

// Params parses the inputs into the transport union via the spec's
// shared parser (provider.Spec.ParseParams — the CLI uses the same one);
// unparsable or empty values are simply not sent.
func (m *Model) Params() provider.Params {
	values := make(map[string]string, len(m.rows))
	for i, row := range m.rows {
		values[row.name] = m.params[i].Value()
	}
	return m.spec.ParseParams(values)
}

// paramLabelW is the widest param label; every label pads to it so the
// side panel's value boxes line up in a column. View, Cursor, and SetSize's
// input sizing all read it so they stay in step.
func (m *Model) paramLabelW() int {
	w := 0
	for _, row := range m.rows {
		w = max(w, len(row.name))
	}
	return w
}
