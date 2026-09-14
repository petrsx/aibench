// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package prefs is settings.json, the app's own file: what aibench
// remembers between runs, edited from the /settings screen and readable —
// hand-editable — like every other dotfile. It sits beside aibench.yaml,
// which stays the human's endpoint file the app never writes; that split
// is why this is not part of internal/config.
//
// Plain JSON on purpose (the shape gh's and Claude Code's CLIs use):
// small, diffable, no comment-preservation problem, no viper. Unknown
// keys are ignored, missing ones take the defaults, and every write
// replaces the file atomically — the app owns it end to end.
package prefs

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/petrsx/aibench/internal/config"
)

// Setting values that are spelled out rather than free text — the app's
// own vocabulary, rotated by the settings screen. An unknown value in
// the file reads as the default.
const (
	ThemeAuto  = "auto" // follow the terminal's background
	ThemeDark  = "dark"
	ThemeLight = "light"

	// StartupLast resumes the profile the last session ended on;
	// anything else is a profile name pinned to start on.
	StartupLast = "last"
)

// Settings is the file's whole shape. ActiveProfile is the one field the
// app maintains on its own (every ctrl+s switch records it); the rest
// are the user's choices, from the settings screen or by hand.
type Settings struct {
	Theme          string `json:"theme"`          // auto | dark | light
	Editor         string `json:"editor"`         // command ctrl+e runs, flags included ("code -w")
	StartupProfile string `json:"startupProfile"` // "last" or a profile name
	UpdateCheck    bool   `json:"updateCheck"`    // ask GitHub for a newer release at launch
	// Log writes the app log (aibench.log, beside this file). Off by
	// default: a developer tool that quietly accumulates a file about
	// your endpoints has to be asked for, not assumed. --debug turns it
	// on for one run whatever this says, which is what makes reporting a
	// problem a single flag rather than a settings trip.
	Log           bool   `json:"log"`
	ActiveProfile string `json:"activeProfile"` // last-used profile
}

// Defaults is what a fresh install behaves like — and the base
// every load starts from, so a file that names only one key keeps the
// rest at their defaults.
func Defaults() Settings {
	return Settings{
		Theme:          ThemeAuto,
		StartupProfile: StartupLast,
		UpdateCheck:    true,
	}
}

// Path is where the file lives: beside the config file, the way
// sessions/ and pricing.yaml do. (The machine-owned internal/ folder
// next to it holds what a human never edits — the schema copy.)
func Path(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "settings.json")
}

// Load reads the file over the defaults. A missing, unreadable,
// or malformed file is not an error — the defaults are the answer, since
// the alternative is refusing to start over a preferences file.
func Load(path string) Settings {
	s := Defaults()
	raw, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return Defaults()
	}
	s.Theme = strings.TrimSpace(s.Theme)
	if s.Theme != ThemeDark && s.Theme != ThemeLight {
		s.Theme = ThemeAuto
	}
	s.Editor = strings.TrimSpace(s.Editor)
	if s.StartupProfile = strings.TrimSpace(s.StartupProfile); s.StartupProfile == "" {
		s.StartupProfile = StartupLast
	}
	return s
}

// Save writes the file atomically (temp + rename, so a crashed
// write never leaves half a file), indented and newline-terminated —
// it is meant to be read and edited by hand too.
func Save(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once the rename succeeded
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// SaveActiveProfile records the profile a switch landed on, leaving
// every other setting as it is.
func SaveActiveProfile(path, name string) error {
	s := Load(path)
	s.ActiveProfile = name
	return Save(path, s)
}

// ActiveProfile picks the profile a session starts on: the pinned one
// when the settings name a profile that still exists, else the last-used
// one, else the first profile in name order (the picker's order), else
// "" when the config has no profiles. A pin that no longer resolves is
// not an error — the yaml is the human's, a profile may have been
// renamed out from under it — it just falls through.
func ActiveProfile(f config.File, settingsPath string) string {
	if len(f.Profiles) == 0 {
		return ""
	}
	s := Load(settingsPath)
	for _, name := range []string{s.StartupProfile, s.ActiveProfile} {
		if name == "" || name == StartupLast {
			continue
		}
		if _, ok := f.Profiles[name]; ok {
			return name
		}
	}
	return slices.Min(slices.Collect(maps.Keys(f.Profiles)))
}

// EditorCommand is the declared editor as argv: the settings file's
// preference (passed in), then $VISUAL and $EDITOR as every terminal
// tool reads them. nil when nothing is declared — the caller decides
// what that means (the TUI asks, the CLI says so). Any of them may
// carry flags ("code -w", "open -t -W", "emacsclient -nw"), so the value
// is split into argv rather than run as one word.
func EditorCommand(pref string) []string {
	for _, c := range []string{pref, os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if argv := strings.Fields(strings.TrimSpace(c)); len(argv) > 0 {
			return argv
		}
	}
	return nil
}
