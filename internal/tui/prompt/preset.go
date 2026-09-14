// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package prompt

import (
	"maps"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/preset"
)

// Preset round-tripping: the prompt file's frontmatter params load into
// the inputs (reload keeps the file authoritative) and easy edits save
// back debounced — the file stays the single source of truth.

// waitPreset re-arms the prompt-file watcher signal; a nil channel (watch
// disabled) blocks forever, which is fine for a tea command.
func (m *Model) waitPreset() tea.Cmd {
	return func() tea.Msg { <-m.presetCh; return ReloadMsg{} }
}

// applyPreset fills the tab from the file: the system markdown into the view,
// params into the inputs by spec name.
func (m *Model) applyPreset(p preset.Preset) {
	m.loaded = p
	m.sysSource = p.System
	// The rows follow the file, so a param appearing or disappearing on
	// disk changes the panel — which is what a hot reload is for.
	m.rows = m.paramRows(p.Params)
	m.buildParamInputs()
	for i, row := range m.rows {
		m.params[i].SetValue(p.Params[row.name])
	}
	m.sizeParamInputs()
	m.renderPreview()
}

// currentPreset is the tab's state as a preset: the (read-only) system markdown
// plus the param inputs merged over the loaded file's params, so foreign
// frontmatter keys survive the round-trip.
func (m *Model) currentPreset() preset.Preset {
	p := preset.Preset{System: m.sysSource, Params: map[string]string{}}
	maps.Copy(p.Params, m.loaded.Params)
	for i, row := range m.rows {
		v := strings.TrimSpace(m.params[i].Value())
		if v == "" {
			// Emptied in the panel means removed from the file — and the
			// row goes with it on the reload that follows.
			delete(p.Params, row.name)
			continue
		}
		p.Params[row.name] = v
	}
	return p
}

// presetEqual reports whether two presets carry the same content.
func presetEqual(a, b preset.Preset) bool {
	if a.System != b.System || len(a.Params) != len(b.Params) {
		return false
	}
	for k, v := range a.Params {
		if b.Params[k] != v {
			return false
		}
	}
	return true
}
