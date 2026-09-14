// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestNoticeIsReadableOverText pins the chip: its own cells blank what is
// under them, so a sentence cannot run straight into the text from either
// side, and the chip runs flush to the pane's right edge — the pad is
// inside it, which is where a reader sees daylight.
func TestNoticeIsReadableOverText(t *testing.T) {
	const width, height = 60, 3
	text := strings.Repeat("x", width)
	block := strings.Join([]string{text, text, text}, "\n")

	last := strings.Split(ansi.Strip(WithNotice(block, "copied", width, height)), "\n")[height-1]
	at := strings.Index(last, "copied")
	if at < 1 {
		t.Fatalf("notice not found (or flush left) in %q", last)
	}
	if last[at-1] != ' ' {
		t.Errorf("no blank before the notice — the text runs into it: %q", last)
	}
	// The chip runs flush to the pane's right edge and keeps its pad
	// inside: the text stops one column short, and that column is the
	// daylight a reader sees before whatever the pane is joined to.
	// (Rendering trims the trailing blank; the column is still the
	// chip's.)
	if got, want := at+len("copied"), width-1; got != want {
		t.Errorf("notice text ends at column %d; want %d — flush chip, pad inside", got, want)
	}
}

// TestNoticeIsTrimmedToThePane pins that a notice never runs off the edge
// it floats over — the renderer knows the width, so it does the cutting.
func TestNoticeIsTrimmedToThePane(t *testing.T) {
	block := strings.Repeat("content\n", 5)
	long := "a notice far too long to fit in a pane this narrow, going on and on"

	out := WithNotice(block, long, 24, 5)
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > 24 {
			t.Fatalf("line %d cells wide in a 24-cell pane: %q", w, ansi.Strip(line))
		}
	}
	if !strings.Contains(ansi.Strip(out), "…") {
		t.Error("a trimmed notice should say it was cut")
	}
}
