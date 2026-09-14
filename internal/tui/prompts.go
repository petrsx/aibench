// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

// The prompt axis, runtime side. A prompt set (instructions + tools +
// starter scripts) belongs to the profile that names it: the profile
// says where requests go and what is being sent, so switching one
// switches the other (profiles.go). There is no command of its own —
// two ways to choose the same thing is one too many.

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/petrsx/aibench/internal/preset"
)

// starterScript is one loaded starter file of the active set: a named,
// runnable scripted conversation.
type starterScript struct {
	Name    string
	Prompts []preset.Starter
}

// setStarters (re)loads the active set's starter scripts: each file is
// one script for /starters, and the flattened prompts seed the
// composer's ↑ history and the empty-transcript welcome list. A file that
// will not load is skipped and named in the log: every path here was
// declared by a prompt set, so its absence is a config error rather than
// an empty list.
func (m *Model) setStarters(paths []string) {
	m.scripts = nil
	m.missingStarters = nil
	var flat []preset.Starter
	for _, path := range paths {
		prompts, err := preset.LoadStarters(path)
		if err != nil {
			slog.Warn("starter script not loaded", "file", path, "err", err)
			m.missingStarters = append(m.missingStarters, path)
			continue
		}
		if len(prompts) == 0 {
			continue
		}
		m.scripts = append(m.scripts, starterScript{Name: scriptName(path), Prompts: prompts})
		flat = append(flat, prompts...)
	}
	m.chat.SetStarters(flat)
	m.registerCommands() // /starters exists only when the set has scripts
}

// scriptName is a starter file's display name: the basename with the
// conventional suffixes stripped (weather.starters.md → weather).
func scriptName(path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".md")
	name = strings.TrimSuffix(name, ".starters")
	return name
}
