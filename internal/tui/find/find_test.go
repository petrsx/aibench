// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package find

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSearch(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		query string
		want  int
	}{
		{"empty query", []string{"hello world"}, "", 0},
		{"blank query trims to empty", []string{"hello"}, "   ", 0},
		{"single match", []string{"the model"}, "model", 1},
		{"case-insensitive, both hit", []string{`"model":"MODEL"`}, "model", 2},
		{"no match", []string{"hello world"}, "zzz", 0},
		{"overlapping steps past each match", []string{"aaaa"}, "aa", 2},
		{"strips ANSI before matching", []string{"\x1b[31mmodel\x1b[0m"}, "model", 1},
		{"matches across lines counted per line", []string{"model a", "b model"}, "model", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(Search(tc.lines, tc.query)); got != tc.want {
				t.Errorf("Search(%q, %q) = %d matches; want %d", tc.lines, tc.query, got, tc.want)
			}
		})
	}
}

func TestSearchSpans(t *testing.T) {
	// Spans are cell columns over the ANSI-stripped line, so a styled
	// haystack still reports coordinates StyleRanges and EnsureVisible speak.
	got := Search([]string{"\x1b[31mabcModelabc\x1b[0m"}, "model")
	if len(got) != 1 {
		t.Fatalf("got %d matches; want 1", len(got))
	}
	want := Match{Line: 0, Start: 3, End: 8}
	if got[0] != want {
		t.Errorf("match = %+v; want %+v (cells over the stripped text)", got[0], want)
	}
}

func TestSearchWideRunes(t *testing.T) {
	// A double-width rune before the match shifts the cell span by two.
	got := Search([]string{"界 model"}, "model")
	if len(got) != 1 {
		t.Fatalf("got %d matches; want 1", len(got))
	}
	if got[0].Start != 3 || got[0].End != 8 {
		t.Errorf("span = [%d %d); want [3 8) after a double-width rune", got[0].Start, got[0].End)
	}
}

// The overlay must not resize as typing changes the match count: the counter
// is padded to the width of "99 of 99" and shows the position, not the count.
func TestViewWidthStable(t *testing.T) {
	m := New()
	_ = m.Open()
	if _, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"}); m.Query() != "a" {
		t.Fatalf("query = %q; want %q", m.Query(), "a")
	}

	width := func() int { return lipgloss.Width(m.View()) }
	m.SetMatches(0)
	w := width()
	if !strings.Contains(ansi.Strip(m.View()), "no match") {
		t.Error("no matches: counter should read \"no match\"")
	}

	m.SetMatches(4)
	m.Next() // 1 of 4 → 2 of 4
	if got := width(); got != w {
		t.Errorf("overlay width changed with matches: %d != %d", got, w)
	}
	if plain := ansi.Strip(m.View()); !strings.Contains(plain, "2 of 4") {
		t.Errorf("counter = %q; want it to show \"2 of 4\"", plain)
	}

	m.SetMatches(42)
	if got := width(); got != w {
		t.Errorf("overlay width changed with matches: %d != %d", got, w)
	}
}

func TestNearest(t *testing.T) {
	matches := []Match{{Line: 2}, {Line: 5}, {Line: 9}}
	for _, tc := range []struct{ top, want int }{
		{0, 0},  // first match below the top
		{3, 1},  // skips matches scrolled past
		{5, 1},  // a match on the top line counts
		{10, 0}, // past them all: wrap to the first
	} {
		if got := Nearest(matches, tc.top); got != tc.want {
			t.Errorf("Nearest(top=%d) = %d; want %d", tc.top, got, tc.want)
		}
	}
	if got := Nearest(nil, 0); got != -1 {
		t.Errorf("Nearest(no matches) = %d; want -1", got)
	}
}

// The regression this package's own highlighting exists for: the bubbles
// viewport's SetHighlights mis-attributes lines on ANSI-styled content (its
// parse walks byte offsets over the stripped text but detects newlines in the
// raw text), so a match on the fifth styled line lit up the third. Search +
// Highlight must land the style on the line the match is actually on.
func TestHighlightLandsOnMatchedLine(t *testing.T) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("63"))
	lines := []string{
		style.Render("alpha first line"),
		style.Render("beta second line"),
		style.Render("gamma third line"),
		style.Render("delta fourth line"),
		style.Render("TARGET fifth line"),
		style.Render("epsilon sixth line"),
	}

	matches := Search(lines, "target")
	if len(matches) != 1 {
		t.Fatalf("got %d matches; want 1", len(matches))
	}
	if matches[0].Line != 4 {
		t.Fatalf("match on line %d; want 4", matches[0].Line)
	}

	baked := Highlight(lines, matches, 0)
	for i := range lines {
		changed := baked[i] != lines[i]
		if changed != (i == 4) {
			t.Errorf("line %d restyled=%v; only the match line (4) may change", i, changed)
		}
		if ansi.Strip(baked[i]) != ansi.Strip(lines[i]) {
			t.Errorf("line %d text changed by Highlight; must restyle only", i)
		}
	}

	// And the viewport shows the baked lines as-is — nothing re-attributes
	// them on the way to the screen.
	vp := viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))
	vp.SetContentLines(baked)
	for i, line := range strings.Split(vp.View(), "\n") {
		if i < len(lines) && ansi.Strip(line) != ansi.Strip(lines[i])+strings.Repeat(" ", 40-len(ansi.Strip(lines[i]))) {
			t.Errorf("visual line %d = %q; want the content line padded to width", i, ansi.Strip(line))
		}
	}
}
