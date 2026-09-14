// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package inspector

// The tab's hints, rotated by the shell (NextTip) and drawn at the right
// end of the tab header. Each one names something the pane does that a
// reader would otherwise have to be told — see chat/tips.go for the
// rule they all follow.

var tips = []string{
	"ctrl+r unescapes the JSON hiding in strings",
	"ctrl+y copies the body exactly as it arrived",
	"shift+↑/↓ walks the requests, no mouse needed",
	"tab flips what went out and what came back",
	"the sizes pane shows what is actually heavy",
}

// NextTip advances the hint shown in the header.
func (m *Model) NextTip() {
	m.tipIdx = (m.tipIdx + 1) % len(tips)
}

// Tip is the hint the shell should show right now. One line: the shell
// docks it against the right edge, so a two-line block moved every time
// the pair's width changed — a thing that jumps as it rotates is read as
// motion, not as a hint.
func (m *Model) Tip() string { return tips[m.tipIdx%len(tips)] }
