// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

// The settings screen (/settings): the app's own preferences, edited in
// place and stored in settings.json beside aibench.yaml. The endpoint
// knobs are NOT here — those are the human's aibench.yaml (ctrl+e opens
// it). What lives here is only what the app itself remembers between
// runs: the theme, the editor, which profile a session starts on, and
// the two background behaviors.
//
// The screen is the dumb widget (internal/tui/screen); this file owns
// what a row means: building them from the current prefs, applying a
// turned value, and writing it back.

import (
	"log/slog"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/applog"
	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/prefs"
	"github.com/petrsx/aibench/internal/tui/screen"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// Row keys — the shell's own vocabulary, matched in applySetting.
const (
	rowTheme   = "theme"
	rowEditor  = "editor"
	rowStartup = "startup"
	rowLog     = "log"
	rowUpdates = "updates"
)

// Display values that are not what gets stored: the two switches, the
// editor's "follow the environment" entry, and the unpinned startup
// choice. Everything else is stored verbatim (the theme names are the
// config package's own).
const (
	valOn       = "on"
	valOff      = "off"
	valAuto     = "auto"      // editor: no declaration, $VISUAL/$EDITOR wins
	valLastUsed = "last used" // startup: resume, do not pin
)

// openSettings builds the screen from the current preferences and takes
// over the content region. focusKey puts the cursor on one row (/profiles-edit
// lands on the editor); "" starts at the top. The Help tab rides along —
// tab reaches it from here, /help opens straight onto it.
func (m *Model) openSettings(focusKey string) {
	s := screen.New(m.settingsRows(), m.settingsNote())
	s.SetHelp(m.renderHelpBody())
	s.Select(focusKey)
	m.settings = &s
	m.layout()
}

// openHelp is /help: the same screen, opened on its Help tab.
func (m *Model) openHelp() {
	m.openSettings("")
	m.settings.ShowHelp()
}

// settingsNote is the screen's subtitle: where these values are kept.
func (m *Model) settingsNote() string {
	return "stored in settings.json, beside aibench.yaml"
}

// settingsRows is the screen's content: one row per preference, current
// value first-class so the screen never has to guess.
func (m *Model) settingsRows() []screen.Row {
	themes := []string{prefs.ThemeAuto, prefs.ThemeDark, prefs.ThemeLight}
	rows := []screen.Row{{
		Key:     rowTheme,
		Label:   "Theme",
		Hint:    "auto follows the terminal background",
		Values:  themes,
		Index:   indexOf(themes, m.prefs.Theme),
		Section: "user",
	}}

	// Editor: whatever is installed, plus auto — which is the ordinary
	// $VISUAL/$EDITOR the rest of the terminal already honors.
	editors := []string{valAuto}
	for _, e := range installedEditors() {
		editors = append(editors, e.cmd)
	}
	rows = append(rows, screen.Row{
		Key:    rowEditor,
		Label:  "Editor",
		Hint:   "/profiles-edit opens aibench.yaml with it · auto = $VISUAL/$EDITOR",
		Values: editors,
		Index:  indexOf(editors, m.prefs.Editor),
	})

	// Startup profile: only meaningful with profiles to choose between.
	if len(m.profileNames) > 0 {
		startup := append([]string{valLastUsed}, m.profileNames...)
		rows = append(rows, screen.Row{
			Key:    rowStartup,
			Label:  "Startup profile",
			Hint:   "which profile a new session opens on",
			Values: startup,
			Index:  indexOf(startup, m.prefs.StartupProfile),
		})
	}

	// The system group: what the app does for itself, apart from the
	// session-shaping settings above.
	rows = append(rows, screen.Row{
		Key:     rowUpdates,
		Label:   "Update check",
		Hint:    "ask GitHub for a newer release at launch",
		Values:  []string{valOn, valOff},
		Index:   switchIndex(m.prefs.UpdateCheck),
		Section: "system",
	})
	rows = append(rows, screen.Row{
		Key:     rowLog,
		Label:   "App log",
		Hint:    "write aibench.log beside the config file; --debug overrides",
		Values:  []string{valOn, valOff},
		Index:   switchIndex(m.prefs.Log),
		Section: "system",
	})
	return rows
}

// updateSettings routes a key into the open screen and applies what it
// reports: a turned row is applied and stored, esc closes.
func (m *Model) updateSettings(msg tea.KeyPressMsg) tea.Cmd {
	s, res, idx := m.settings.Update(msg)
	m.settings = &s
	switch res {
	case screen.Closed:
		m.settings = nil
		m.layout()
	case screen.Changed:
		return m.applySetting(s.Rows()[idx])
	}
	return nil
}

// applySetting folds one turned row into the preferences, makes it true
// right now where that is possible, and writes the set back.
func (m *Model) applySetting(row screen.Row) tea.Cmd {
	var cmd tea.Cmd
	switch row.Key {
	case rowTheme:
		m.prefs.Theme = row.Value()
		m.applyThemePref()
	case rowEditor:
		m.prefs.Editor = row.Value()
		if m.prefs.Editor == valAuto {
			m.prefs.Editor = "" // unset: back to $VISUAL/$EDITOR
		}
	case rowStartup:
		m.prefs.StartupProfile = row.Value()
		if m.prefs.StartupProfile == valLastUsed {
			m.prefs.StartupProfile = prefs.StartupLast
		}
	case rowUpdates:
		m.prefs.UpdateCheck = row.Value() == valOn
	case rowLog:
		// True right now, not next launch: turning it on to capture
		// something and having to restart first is how a report ends up
		// with the interesting run missing.
		m.prefs.Log = row.Value() == valOn
		if _, err := applog.Init(m.prefs.Log, false); err != nil {
			cmd = m.notifyError("logging: " + err.Error())
		}
	}
	m.saveSettings()
	return cmd
}

// saveSettings writes the file, beside the config.
func (m *Model) saveSettings() {
	if err := prefs.Save(prefs.Path(config.FilePath()), m.prefs); err != nil {
		slog.Warn("settings not saved", "err", err)
	}
}

// applyThemePref repaints in the preferred palette: an explicit choice
// wins, auto follows what the terminal reported (dark until it answers).
func (m *Model) applyThemePref() {
	dark := m.termDark
	switch m.prefs.Theme {
	case prefs.ThemeDark:
		dark = true
	case prefs.ThemeLight:
		dark = false
	}
	theme.Apply(dark)
	m.applyTheme()
	m.layout()
}

// indexOf is the position of value in values, 0 (the default row) when
// it is not one of them — a hand-edited state file cannot break the
// screen.
func indexOf(values []string, value string) int {
	for i, v := range values {
		if v == value {
			return i
		}
	}
	return 0
}

// switchIndex maps a bool onto the on/off pair.
func switchIndex(on bool) int {
	if on {
		return 0
	}
	return 1
}

// applyTheme re-captures the theme in styles that components copied by value
// at construction (help line, spinner, the Inspector's highlight styles) —
// theme.Apply reassigns the package vars, but copies don't follow.
func (m *Model) applyTheme() {
	m.hlp.Styles.ShortKey = theme.HelpKeyStyle
	m.hlp.Styles.ShortDesc = theme.HelpDescriptionStyle
	m.hlp.Styles.ShortSeparator = theme.HelpDescriptionStyle
	m.hlp.Styles.FullKey = theme.HelpKeyStyle
	m.hlp.Styles.FullDesc = theme.HelpDescriptionStyle
	m.hlp.Styles.FullSeparator = theme.HelpDescriptionStyle
	m.state.SetSpinnerStyle(theme.SpinnerStyle)
	m.chat.ApplyTheme()
	m.inspector.ApplyTheme()
	m.prompt.ApplyTheme()
}
