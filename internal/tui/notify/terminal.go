// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package notify

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// Terminal announces out-of-band that a reply landed while the window was
// unfocused: the terminal bell (audible/visual per the user's terminal
// config — works everywhere) plus an OSC 9 desktop notification (iTerm2,
// WezTerm, Ghostty, kitty, Windows Terminal; terminals without it consume
// the sequence silently per spec).
func Terminal(message string) tea.Cmd {
	return tea.Raw("\x07" + fmt.Sprintf("\x1b]9;%s\x07", message))
}
