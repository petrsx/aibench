// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

import (
	"strings"
	"testing"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// TestCompactNum pins the axis-label abbreviation: narrow columns, no
// pointless precision, and no trailing ".0" on round values.
func TestCompactNum(t *testing.T) {
	for in, want := range map[int]string{
		0:         "0",
		999:       "999",
		1000:      "1k",
		1500:      "1.5k",
		10_000:    "10k",
		400_000:   "400k",
		999_999:   "1000k", // still k: the M step starts at a million
		1_000_000: "1M",
		2_500_000: "2.5M",
		-1500:     "-1.5k",
	} {
		if got := TokensCompact(in); got != want {
			t.Errorf("TokensCompact(%d) = %q; want %q", in, got, want)
		}
	}
}

// TestAxisNum pins the Tokens-axis abbreviation: three cells at most for
// every value the scale can take, via whole units and the zero-dropped
// tenths-of-a-meg band.
func TestAxisNum(t *testing.T) {
	for in, want := range map[int]string{
		0:          "0",
		999:        "999",
		1000:       "1k",
		2500:       "3k", // half of a 5k scale, rounded whole
		50_000:     "50k",
		99_600:     ".1M", // rounds past 99k, so it jumps to the tenths band
		100_000:    ".1M",
		250_000:    ".3M", // half of a 500k scale, rounded to a tenth
		400_000:    ".4M",
		500_000:    ".5M",
		1_000_000:  "1M",
		2_500_000:  "3M",
		10_000_000: "10M",
		-2500:      "-3k",
	} {
		if got := TokensAxis(in); got != want {
			t.Errorf("TokensAxis(%d) = %q; want %q", in, got, want)
		}
	}
}

// TestScrollBar pins the column: blank when everything fits (so a pane can
// reserve it without its width shifting as content grows), and otherwise a
// thumb whose size tracks the visible fraction and whose position tracks
// the offset — top at 0, bottom at the end.
func TestScrollBar(t *testing.T) {
	if got := ScrollBar(4, 3, 4, 0); strings.Join(got, "") != "    " {
		t.Errorf("content fits: bar = %q; want blanks", got)
	}
	if got := ScrollBar(0, 100, 10, 0); got != nil {
		t.Errorf("zero height: bar = %q; want nil", got)
	}

	top := ScrollBar(10, 100, 10, 0)
	if top[0] != "▌" {
		t.Errorf("at offset 0 the thumb must start at the top: %q", top)
	}
	bottom := ScrollBar(10, 100, 10, 90)
	if bottom[len(bottom)-1] != "▌" {
		t.Errorf("at the end the thumb must reach the bottom: %q", bottom)
	}
	// A tiny visible fraction still gets a visible thumb.
	if n := strings.Count(strings.Join(ScrollBar(10, 10000, 10, 0), ""), "▌"); n != 1 {
		t.Errorf("thumb cells = %d; want exactly 1 for a tiny fraction", n)
	}
}

// TestCompactBytes pins the header form: decimal thousands with a B, since
// these are wire sizes eyeballed for scale, not disk blocks.
func TestCompactBytes(t *testing.T) {
	for in, want := range map[int64]string{
		0: "0 B", 999: "999 B", 1000: "1 kB", 10603: "10.6 kB", 1_500_000: "1.5 MB",
	} {
		if got := Bytes(in); got != want {
			t.Errorf("Bytes(%d) = %q; want %q", in, got, want)
		}
	}
}

// TestWithScrollBarColors pins that the two parts are styled differently:
// the track wears the frame color so it reads as part of the border, the
// thumb wears the accent so the moving part is what the eye finds.
func TestWithScrollBarColors(t *testing.T) {
	theme.Apply(true)
	out := WithScrollBar("a\nb\nc\nd", 4, 100, 4, 0)

	thumb := theme.ScrollThumbStyle.Render(scrollThumb)
	track := theme.FrameStyle.Render(scrollTrack)
	if !strings.Contains(out, thumb) {
		t.Errorf("scrollbar thumb is not accent-styled:\n%q", out)
	}
	if !strings.Contains(out, track) {
		t.Errorf("scrollbar track is not frame-styled:\n%q", out)
	}
	if thumb == track {
		t.Error("thumb and track render identically; the moving part must stand out")
	}
}
