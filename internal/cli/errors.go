// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Error presentation for the command surface. Every failure that stops a
// command funnels through cobra's single Execute error (SilenceErrors is
// on), so this file is the one place a pre-TUI failure gets its shape.
//
// Once the TUI is up it reports its own problems as in-app notices (a
// profile that won't resolve, a send that fails) and none of this is
// used — so a message in this format always means "the screen never
// opened", which is exactly the distinction a reader needs. Warnings the
// launch path raises after that point go to slog instead: the alt-screen
// wipes stderr, and aibench.log is where they stay readable.
//
// Errors are multi-line by design: the first line says what failed, the
// rest is the help that makes it fixable (see config.CredentialHelp).
// This file pins how that block looks; it never composes the text.
package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// Styles match the TUI's palette (theme color 62, red for errors) so the
// same problem reads the same whether it lands on stderr or in a notice.
var (
	errLabel  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	warnLabel = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	detail    = lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
)

// printError writes err to stderr in the standard shape and is the only thing
// the cobra layer needs on a failed command.
func printError(err error) { writeError(os.Stderr, err) }

// printWarn reports something degraded but survivable — a watcher that could
// not be armed, logging that could not start. The run continues.
func printWarn(format string, a ...any) { writeWarn(os.Stderr, format, a...) }

// writeError renders the standard error block to w:
//
//	error: what failed
//
//	  the help that makes it fixable, indented as one block
//
// Styling is decided by w: a terminal gets color, a pipe or NO_COLOR gets
// clean text, so the format is safe to redirect into a bug report.
func writeError(w io.Writer, err error) {
	if err == nil {
		return
	}
	out := colorprofile.NewWriter(w, os.Environ())
	summary, rest := split(err.Error())
	fmt.Fprintf(out, "%s %s\n", errLabel.Render("error:"), summary)
	writeDetail(out, rest)
}

// writeWarn renders the warning form: one styled line, no detail block.
func writeWarn(w io.Writer, format string, a ...any) {
	out := colorprofile.NewWriter(w, os.Environ())
	summary, rest := split(fmt.Sprintf(format, a...))
	fmt.Fprintf(out, "%s %s\n", warnLabel.Render("warning:"), summary)
	writeDetail(out, rest)
}

// split separates the first line — the summary that must stand alone in a
// CI log — from the help that follows.
func split(msg string) (summary, rest string) {
	msg = strings.TrimRight(msg, "\n")
	if i := strings.Index(msg, "\n"); i >= 0 {
		return msg[:i], strings.TrimLeft(msg[i+1:], "\n")
	}
	return msg, ""
}

// writeDetail indents the help under its summary so a multi-line message
// reads as one block rather than several unrelated lines. Blank lines
// stay blank — trailing whitespace in a terminal is noise.
func writeDetail(w io.Writer, rest string) {
	if rest == "" {
		return
	}
	fmt.Fprintln(w)
	sc := bufio.NewScanner(strings.NewReader(rest))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if line := sc.Text(); line != "" {
			fmt.Fprintln(w, detail.Render("  "+line))
		} else {
			fmt.Fprintln(w)
		}
	}
	fmt.Fprintln(w)
}
