// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

// The profile strip: what Chat and Prompt put in their tab header. The two
// axes plus where requests go are app-global state the shell owns, so the
// shell builds the line and hands the finished string to the tabs — they
// render it, they do not assemble it. (The Inspector fills the same header
// with its record head instead: the box is chrome, its content is the
// tab's business.)

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// profileLine is the strip — labeled fields, pipe-separated:
// profile: a | api: b | model: c | prompt: d | host: e. Empty fields drop
// out; the host is a real link (OSC 8) where the terminal supports it, and
// rides last so the header's end-truncation trims the one field that can
// grow arbitrarily long. Recomputed and handed down on every profile or
// prompt switch.
func (m *Model) profileLine() string {
	host := strings.TrimPrefix(strings.TrimPrefix(m.cfg.APIBase, "https://"), "http://")
	if m.cfg.APIBase != "" {
		host = lipgloss.NewStyle().Hyperlink(m.cfg.APIBase).Render(host)
	}
	var parts []string
	add := func(label, v string) {
		if v != "" {
			parts = append(parts, theme.MetaKeyStyle.Render(label+": ")+v)
		}
	}
	add("profile", m.activeProfile)
	add("api", m.cfg.API)
	add("model", m.cfg.Model)
	add("prompt", m.activePrompt)
	add("host", host)
	return " " + strings.Join(parts, theme.DimStyle.Render(" | "))
}
