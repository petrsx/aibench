// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestPrintErrorShape pins the standard block: a summary line that stands
// alone in a CI log, then the help indented under it as one unit.
func TestPrintErrorShape(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var b bytes.Buffer
	writeError(&b, errors.New("no credentials in .env.dev\n\nset one of these:\n  OPENAI_API_KEY=…"))

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if lines[0] != "error: no credentials in .env.dev" {
		t.Errorf("summary line = %q; want the error's first line after an \"error:\" label", lines[0])
	}
	if lines[1] != "" {
		t.Errorf("line 2 = %q; want a blank line separating summary from help", lines[1])
	}
	for _, want := range []string{"  set one of these:", "    OPENAI_API_KEY=…"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("help missing %q (indented under the summary); got:\n%s", want, b.String())
		}
	}
}

// TestPrintErrorSingleLine keeps the common case terse: no trailing blank
// lines when there is no help to show.
func TestPrintErrorSingleLine(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var b bytes.Buffer
	writeError(&b, errors.New("profile \"x\" not in config"))
	if got := b.String(); got != "error: profile \"x\" not in config\n" {
		t.Errorf("output = %q; want exactly one labeled line", got)
	}
}

// TestPrintErrorNil is the no-op guard: a nil error prints nothing, so
// callers need no if.
func TestPrintErrorNil(t *testing.T) {
	var b bytes.Buffer
	writeError(&b, nil)
	if b.Len() != 0 {
		t.Errorf("output = %q; want nothing for a nil error", b.String())
	}
}

// TestNoColorWhenRedirected pins the redirect contract: output piped into
// a file or a bug report carries no escape sequences.
func TestNoColorWhenRedirected(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var b bytes.Buffer
	writeError(&b, errors.New("boom\n\ndetail"))
	writeWarn(&b, "watch disabled: %v", errors.New("nope"))
	if strings.Contains(b.String(), "\x1b[") {
		t.Errorf("output carries ANSI escapes when redirected:\n%q", b.String())
	}
	if !strings.Contains(b.String(), "warning: watch disabled: nope") {
		t.Errorf("warning form missing its label; got:\n%s", b.String())
	}
}
