// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// TestTokenRowsWearTheGraphColours pins the one distinction in this panel
// a colour can carry: in and out, in the same pair the Tokens graph and
// the record lines already use, so a reader learns it once. The totals
// stay plain — a sum of two coloured things is neither.
func TestTokenRowsWearTheGraphColours(t *testing.T) {
	m := newChatModel()
	addRecord(m.state, 200, &provider.Usage{Prompt: 565, Completion: 30, Total: 595})
	m.renderMetrics()

	body := strings.Join(m.metricsLines, "\n")
	if !strings.Contains(body, theme.TokensInStyle.Render("565")) {
		t.Error("the in row does not wear the graph's in colour")
	}
	if !strings.Contains(body, theme.TokensOutStyle.Render("30")) {
		t.Error("the out row does not wear the graph's out colour")
	}
	for _, line := range m.metricsLines {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "total") {
			continue
		}
		if strings.Contains(line, theme.TokensInStyle.Render("595")) ||
			strings.Contains(line, theme.TokensOutStyle.Render("595")) {
			t.Errorf("the total borrowed an in/out colour: %q", plain)
		}
	}
}
