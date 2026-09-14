// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestHeaderCarriesTheTip pins where the hint lives: one line,
// right-docked on the tab bar's row, labelled, and nowhere near the
// wordmark's row — which stays clear for the tab bar to grow into.
func TestHeaderCarriesTheTip(t *testing.T) {
	const width = 120
	names := [tabCount]string{"Chat", "Inspector", "Prompt"}
	head, _ := renderHeader(width, 0, names, "the bars are buttons", "")
	rows := strings.Split(head, "\n")
	if len(rows) != 3 {
		t.Fatalf("header rows = %d; want 3", len(rows))
	}
	if got := ansi.Strip(rows[1]); !strings.Contains(got, "tip: the bars are buttons") {
		t.Errorf("the labelled hint is not on the tab bar's row: %q", got)
	}
	if got := strings.TrimSpace(ansi.Strip(rows[0])); !strings.HasSuffix(got, "bench") {
		t.Errorf("the wordmark's row carries something on the right: %q", got)
	}
	for i, row := range rows {
		if got := lipgloss.Width(row); got != width {
			t.Errorf("row %d width = %d; want %d", i, got, width)
		}
	}
}

// TestHeaderDropsATipItCannotHold pins the hint's place in the pecking
// order: the tab bar owns that row, and a hint too wide for what is left
// is dropped whole rather than truncated to a fragment.
func TestHeaderDropsATipItCannotHold(t *testing.T) {
	names := [tabCount]string{"Chat", "Inspector", "Prompt"}
	head, _ := renderHeader(60, 0, names, "a hint far too long for a narrow terminal", "")
	rows := strings.Split(head, "\n")
	if strings.Contains(ansi.Strip(rows[1]), "a hint") {
		t.Errorf("a hint that does not fit was drawn anyway: %q", ansi.Strip(rows[1]))
	}
	if !strings.Contains(ansi.Strip(rows[1]), "Chat") {
		t.Errorf("the tab bar lost its row to the hint: %q", ansi.Strip(rows[1]))
	}
}

// TestCreditGivesWayInOrder pins the lockup's retreat: version and link,
// then the version alone, then nothing — and the link is a real OSC 8
// hyperlink whose sequences measure zero, so the row it sits on is
// exactly as wide as the text reads.
func TestCreditGivesWayInOrder(t *testing.T) {
	v := versionLabel()
	full := v + " · " + repoURL

	got := credit(lipgloss.Width(full))
	if !strings.Contains(got, ansi.SetHyperlink("https://"+repoURL)) {
		t.Error("the project link is not an OSC 8 hyperlink")
	}
	if !strings.Contains(got, ansi.ResetHyperlink()) {
		t.Error("the hyperlink is never closed; the rest of the row would inherit it")
	}
	if plain := ansi.Strip(got); plain != full {
		t.Errorf("full lockup reads %q; want %q", plain, full)
	}
	if w := lipgloss.Width(got); w != lipgloss.Width(full) {
		t.Errorf("the hyperlink sequences measure %d cells too many", w-lipgloss.Width(full))
	}

	if got := credit(lipgloss.Width(full) - 1); got != v {
		t.Errorf("one cell short: %q; want the version alone (%q)", ansi.Strip(got), v)
	}
	if got := credit(lipgloss.Width(v) - 1); got != "" {
		t.Errorf("too narrow for the version: %q; want nothing", ansi.Strip(got))
	}
}

// TestHelpLineDocksTheCredit pins that the lockup rides the help line's
// right end and never costs the controls a cell.
func TestHelpLineDocksTheCredit(t *testing.T) {
	m := newTestModel()
	line := m.helpLine()
	plain := ansi.Strip(line)
	if !strings.Contains(plain, "enter send") {
		t.Fatalf("the help line lost its controls: %q", plain)
	}
	if !strings.Contains(plain, repoURL) {
		t.Errorf("the credit is not on the help line at %d columns: %q", m.width, plain)
	}
	if got := lipgloss.Width(line); got > m.width {
		t.Errorf("help line width = %d; want at most %d", got, m.width)
	}

	// Narrow enough that the lockup cannot fit at all: the controls stay,
	// the credit goes.
	m.width = 60
	if plain := ansi.Strip(m.helpLine()); strings.Contains(plain, repoURL) {
		t.Errorf("the credit crowded the controls at 60 columns: %q", plain)
	}
}

// TestUpdateNewsTakesTheTipSlot pins the swap: while a newer build is
// known, the header says so where the rotating hint would be. A hint is
// worth reading and an update is worth doing, so the news takes the slot
// and does not share it.
func TestUpdateNewsTakesTheTipSlot(t *testing.T) {
	names := [tabCount]string{"Chat", "Inspector", "Prompt"}
	head, _ := renderHeader(120, 0, names, "the bars are buttons", "v9.9.9 · brew upgrade petrsx/tap/aibench")
	row := ansi.Strip(strings.Split(head, "\n")[1])
	if !strings.Contains(row, "update: v9.9.9 · brew upgrade petrsx/tap/aibench") {
		t.Errorf("the update news is not on the tab bar's row: %q", row)
	}
	if strings.Contains(row, "tip:") || strings.Contains(row, "the bars are buttons") {
		t.Errorf("the hint and the news are sharing the slot: %q", row)
	}
}
