// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package prompt

// The system-prompt preview: the raw markdown rendered with glamour into the
// read-only scroll view, re-derived whenever the source, width, or palette
// changes.

import (
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// markdownStyle is glamour's standard dark/light config with one change: inline
// code (backtick spans) ships as red text on a grey block, which clashes with
// the app palette — drop the background and tint it with the app accent so
// `GET …` and the like read as quiet code, not warnings.
func markdownStyle() ansi.StyleConfig {
	cfg := styles.DarkStyleConfig
	link := "#7dd3fc" // light sky blue: high-contrast on a dark background
	if !theme.Dark {
		cfg = styles.LightStyleConfig
		link = "#1d4ed8" // deep blue: readable on white
	}
	accent := "62" // theme.accent — holds up on both backgrounds
	cfg.Code = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: &accent}}
	// The default link color (dark teal 30) is too low-contrast to read; use a
	// brighter blue keyed to the background, keeping the underline.
	underline := true
	cfg.Link = ansi.StylePrimitive{Color: &link, Underline: &underline}
	return cfg
}

// renderPreview re-renders the system markdown as styled terminal output into
// the view, wrapped to the current width and matching the light/dark palette.
// An empty prompt shows a hint; a glamour error falls back to the raw text.
func (m *Model) renderPreview() {
	w := max(1, m.sysView.Width())
	if strings.TrimSpace(m.sysSource) == "" {
		// Three different silences, and telling them apart is the point: a
		// declared file that is not there used to read exactly like a
		// profile with nothing configured, so a session could send bare
		// messages while the header claimed a prompt set.
		hint := "  No system prompt — edit the prompt file to add one."
		switch {
		case m.promptFile == "":
			hint = "  Instructions and tools live in the agent endpoint (kind: agent) — no prompt file applies."
		case m.loadErr != nil:
			hint = "  Not found: " + m.promptFile + " — the config names it, but it is not there."
		}
		m.sysLines = []string{theme.DimStyle.Render(hint)}
		m.sysView.SetContentLines(m.sysLines)
		if m.find.Active() {
			m.find.Apply(m) // fresh content: recompute the matches
		}
		m.sysView.GotoTop()
		return
	}

	out := m.sysSource
	if r, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyle()),
		glamour.WithWordWrap(w),
	); err == nil {
		if s, err := r.Render(m.sysSource); err == nil {
			out = s
		}
	}
	m.sysLines = strings.Split(strings.Trim(out, "\n"), "\n")
	m.sysView.SetContentLines(m.sysLines)
	if m.find.Active() {
		m.find.Apply(m) // fresh content: recompute the matches
	}
}
