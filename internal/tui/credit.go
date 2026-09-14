// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

// Where this build came from: the version and the project's home. They
// ride the right end of the help line — the row that is always there and
// never full — rather than the header, where every cell competes with
// the tab bar and the hint.
//
// The lockup gives way in one direction: version and link, then version
// alone, then nothing. What it must never do is push on the help line,
// which is the one row telling a reader what they can press.

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/version"
)

// repoURL is the project's home, clickable where the terminal supports
// OSC 8 (most do; the rest simply read it as text).
const repoURL = render.RepoURL

// versionLabel is the build, spelled the way a release is (v1.2.3) —
// except a dev build, which is called what it is.
func versionLabel() string {
	v := version.Version
	if v != "dev" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

// credit is the widest lockup that fits in room cells: the version and
// the link, else the version alone, else nothing at all.
func credit(room int) string {
	v := versionLabel()
	if full := v + " · " + repoURL; lipgloss.Width(full) <= room {
		return v + " · " + render.Link(repoURL, repoURL)
	}
	if lipgloss.Width(v) <= room {
		return v
	}
	return ""
}
