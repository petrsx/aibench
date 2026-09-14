// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

// Terminal hyperlinks. A pane that names a document should be able to
// open it — the sequence is zero-width, so a link costs a view nothing
// it would not have spent on the text anyway, and a terminal that does
// not support OSC 8 simply shows the words.

import "github.com/charmbracelet/x/ansi"

// RepoURL is the project's home. The docs are read from it rather than
// from a checkout, since the app is installed as a binary far more often
// than it is cloned.
const RepoURL = "github.com/petrsx/aibench"

// Link wraps text in an OSC 8 hyperlink to an https uri given without
// its scheme.
func Link(uri, text string) string {
	return ansi.SetHyperlink("https://"+uri) + text + ansi.ResetHyperlink()
}

// DocsLink is Link to a page of the project's docs, optionally at one of
// its headings.
func DocsLink(text, page, anchor string) string {
	uri := RepoURL + "/blob/main/docs/" + page
	if anchor != "" {
		uri += "#" + anchor
	}
	return Link(uri, text)
}
