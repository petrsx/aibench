// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package chat

// The tab's hints. They ride the right end of the tab header, one at a
// time, and the shell rotates them (NextTip) so the cadence is the same
// on every tab. A tip earns its place by naming something the screen
// does not already say: a gesture, or a key that is not on the help
// line. Nothing here is a substitute for /help — these are the things
// you would otherwise have to be told.

// tips is the rotation, in the order a reader meets the tab: what the
// mouse does first, then the keys that change what the panels show.
var tips = []string{
	"the bars in the graph are buttons — click one",
	"click a record line and the panels follow",
	"shift+↑/↓ walks the requests, no mouse needed",
	"ctrl+d hides the plumbing, keeps the chat",
	"ctrl+y copies the whole conversation",
	"/starters replays a chat, no typing",
	"nothing auto-sends — the app is not that brave",
}

// NextTip advances the hint shown in the header. Called by the shell on
// its timer, and only for the tab in front — a tab nobody is looking at
// has nothing to say.
func (m *Model) NextTip() {
	m.tipIdx = (m.tipIdx + 1) % len(tips)
}

// Tip is the hint the shell should show right now. One line: the shell
// docks it against the right edge, so a two-line block moved every time
// the pair's width changed — a thing that jumps as it rotates is read as
// motion, not as a hint.
func (m *Model) Tip() string { return tips[m.tipIdx%len(tips)] }
