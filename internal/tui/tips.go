// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

// The header tips' cadence. The shell owns when they turn over; what
// each one says belongs to the tab it is about (chat/tips.go and its
// two siblings), the way each tab owns its keys. Only the tab in front
// advances: a hint nobody can see has nothing to say, and rotating the
// other two would land a reader on a mid-cycle tip when they switch.

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// tipEvery is how long one hint stays up. Long enough to read twice
// without effort, short enough that a session shows a few of them.
const tipEvery = 15 * time.Second

type tipTickMsg struct{}

func tipTick() tea.Cmd {
	return tea.Tick(tipEvery, func(time.Time) tea.Msg { return tipTickMsg{} })
}
