// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

// The shell's notices. A tab's own notice answers a click in its pane —
// "copied to clipboard" belongs where you clicked. The shell's messages
// are about the app: a profile that would not resolve, a conversation
// cleared, an editor that would not open. Those went to the Chat tab
// whichever tab you were on, so a reader on the Inspector never saw them.
//
// They go to the tab in front now. Where a notice sits stays the tab's
// business — over its main content, which is where the eye already is —
// and what the shell gains is that its messages follow the reader.

import (
	"log/slog"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/tui/theme"
)

// notify shows a pre-styled line in the active tab until it lapses.
func (m *Model) notify(text string) tea.Cmd {
	return m.tabs[m.activeTab].model.Notify(text)
}

// headline reduces an error to what a notice can say: its first line.
// The errors this app raises are written to teach — the credentials help
// lists every method you could set, and runs to a dozen lines — which is
// right in a terminal that keeps scrollback and wrong in a notice that
// paints over the transcript for four seconds. The first line is the
// summary those errors already lead with ("no credentials in .env.dev");
// the rest waits in the log.
//
// Nothing here counts columns: how much of one line fits is the pane's
// business, and the renderer trims to it (render.WithNoticeIn).
func headline(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text)
}

// notifyError, notifyWarn and notifySay are the three voices the shell
// uses, named so a caller picks a meaning rather than a colour.
func (m *Model) notifyError(text string) tea.Cmd {
	// The whole error goes to the log; the notice gets the headline, and
	// says where the rest is the one time it had to drop any.
	slog.Error("notice", "err", text)
	short := headline(text)
	if short != strings.TrimSpace(text) {
		short += " · see aibench.log"
	}
	return m.notify(theme.ErrorStyle.Render(short))
}

func (m *Model) notifyWarn(text string) tea.Cmd {
	return m.notify(theme.WarningStyle.Render(text))
}

func (m *Model) notifySay(text string) tea.Cmd {
	return m.notify(theme.NoticeStyle.Render(text))
}
