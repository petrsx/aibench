// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package prompt

// The tab's hints, rotated by the shell (NextTip) and drawn at the right
// end of the tab header — see chat/tips.go for the rule they follow.

var tips = []string{
	"params live in the file's frontmatter",
	"a param you leave out is never sent",
	"params autosave — no button to forget",
	"edit the file; the app reloads it unasked",
	"tools merge in verbatim: what you write is sent",
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
