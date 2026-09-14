// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package e2e

// Golden frames, cut into the pieces the layout is actually made of.
//
// A whole 120×35 frame in one file meant every golden rewrote whenever
// anything moved a column — a change to the transcript churned the
// settings frame — and the diff was 120-wide lines of escape codes, which
// nobody reads carefully twice. So:
//
//   - each region of the frame is its own golden (goldenColumns runs one
//     subtest per region, so the file is named for it and `-run` can
//     regenerate exactly one),
//   - the text is ANSI-stripped, because layout is what these pin; where
//     styling is the point, assert the escape inline instead (see
//     TestChatFind, which checks its highlight band directly),
//   - trailing blanks go, so an empty transcript is not fifteen rows of
//     nothing in the diff.
//
// Regenerate: `go test ./internal/tui/e2e -update`, or one region with
// `go test ./internal/tui/e2e -run 'TestGoldenInitialFrame/side' -update`.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/petrsx/aibench/internal/tui/render"
)

// goldenWidth is the width the golden tests render at; the column split
// below is derived from it exactly as the tabs derive theirs.
const goldenWidth = 120

// crop takes the cell columns [x0, x1) of every row, ANSI-stripped, with
// each row's trailing blanks and the block's trailing blank rows removed.
func crop(frame string, x0, x1 int) string {
	var out []string
	for _, l := range strings.Split(frame, "\n") {
		out = append(out, strings.TrimRight(ansi.Cut(ansi.Strip(l), x0, x1), " "))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// rows takes whole rows [y0, y1) of a frame. A negative bound counts back
// from the end, so the help line is rows(frame, -2, -1) whatever the
// terminal's height.
func rows(frame string, y0, y1 int) string {
	lines := strings.Split(frame, "\n")
	from := func(y int) int {
		if y < 0 {
			y += len(lines) + 1
		}
		return max(0, min(y, len(lines)))
	}
	y0, y1 = from(y0), from(y1)
	if y0 >= y1 {
		return ""
	}
	return strings.Join(lines[y0:y1], "\n")
}

// goldenColumns pins a tab frame as its regions: the shell's header, the
// main column, the side panel, and the help line. A change to one lands in
// one file, which is the whole point.
func goldenColumns(t *testing.T, frame string) {
	t.Helper()
	main, _ := render.SplitWidths(goldenWidth)
	for _, r := range []struct {
		name string
		text string
	}{
		{"header", crop(rows(frame, 0, 3), 0, goldenWidth)},
		{"main", crop(rows(frame, 3, -2), 0, main)},
		{"side", crop(rows(frame, 3, -2), main, goldenWidth)},
		{"help", crop(rows(frame, -2, -1), 0, goldenWidth)},
	} {
		t.Run(r.name, func(t *testing.T) {
			golden.RequireEqual(t, []byte(r.text))
		})
	}
}

// goldenScreen pins a frame that owns the whole width — the settings
// screen, the key sheet — where a column split would say nothing.
func goldenScreen(t *testing.T, frame string) {
	t.Helper()
	golden.RequireEqual(t, []byte(crop(frame, 0, goldenWidth)))
}
