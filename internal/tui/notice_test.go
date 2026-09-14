// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

import (
	"strings"
	"testing"
)

// TestHeadlineFitsANotice pins what a notice may carry: one line, capped.
// The credentials help is the error that forced this — it teaches by
// listing every method you could set, which is right in a terminal and
// wrong painted over the transcript for four seconds.
func TestHeadlineFitsANotice(t *testing.T) {
	long := "no credentials in .env.dev\n\nit defines MY_KEY, which no auth method reads\n\n" +
		"set one of these in it:\n  OPENAI_API_KEY=…    an API key the endpoint issued"
	if got := headline(long); got != "no credentials in .env.dev" {
		t.Errorf("headline() = %q; want just the summary line", got)
	}
	if got := headline("profile \"p\" not in config"); got != `profile "p" not in config` {
		t.Errorf("headline() reshaped a line that already fits: %q", got)
	}
	// Length is not this function's business: a long single line comes
	// back whole, and the pane trims it (render.WithNoticeIn).
	wide := strings.Repeat("x", 200)
	if got := headline(wide); got != wide {
		t.Errorf("headline() shortened a single line; that is the renderer's job")
	}
	if got := headline("  padded  \n more "); got != "padded" {
		t.Errorf("headline() = %q; want it trimmed", got)
	}
}
