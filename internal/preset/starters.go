// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package preset

import (
	"os"
	"strings"
)

// The starter-prompts file (the industry's "starter prompts" /
// "suggested prompts": canned user messages, editable before sending),
// declared per profile in aibench.yaml (`prompt.starters:` — one line to
// comment out). Markdown: every `## title` heading starts one prompt,
// its body (until the next heading) is the message text. Content before
// the first heading is ignored, so a title or notes can live up top. The
// composer seeds its input history from it (up-arrow walks the entries)
// and `aibench send --script` plays it in order.

// Starter is one starter prompt: Title is the `##` heading (for
// display), Text the body that gets sent.
type Starter struct {
	Title string
	Text  string
}

// LoadStarters parses the starter-prompts file. An empty path means no
// starters; a path that names a file which is not there is an error, since
// only a config that declared it can produce one (see Load).
func LoadStarters(path string) ([]Starter, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var starters []Starter
	var cur *Starter
	var body []string
	flush := func() {
		if cur == nil {
			return
		}
		if text := strings.TrimSpace(strings.Join(body, "\n")); text != "" {
			starters = append(starters, Starter{Title: cur.Title, Text: text})
		}
		cur, body = nil, nil
	}
	for line := range strings.Lines(string(raw)) {
		if name, ok := strings.CutPrefix(line, "## "); ok {
			flush()
			cur = &Starter{Title: strings.TrimSpace(name)}
			continue
		}
		if cur != nil {
			body = append(body, strings.TrimRight(line, "\n"))
		}
	}
	flush()
	return starters, nil
}
