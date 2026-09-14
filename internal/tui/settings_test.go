// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/prefs"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/tui/chat"
)

// arrow builds one of the screen's navigation keys.
func arrow(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;:?]*[a-zA-Z]`)

// plain is the current frame with the styling stripped — styles split
// words across escape sequences, so raw frames don't match on text.
func plain(m *Model) string { return ansiRE.ReplaceAllString(m.View().Content, "") }

// TestSettingsScreen pins the /settings flow end to end: the screen opens
// over the tab, a turned row applies and is written to settings.json,
// and esc closes it.
func TestSettingsScreen(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "aibench.yaml")
	t.Setenv("CONFIG_FILE", cfgPath)
	t.Setenv("OPENAI_API_KEY", "k")

	m := newTestModel()
	m.EnableProfiles(config.File{Profiles: map[string]config.Profile{
		"alpha": {Kind: config.KindModel},
		"bravo": {Kind: config.KindModel},
	}}, "alpha", func(config.Config) (provider.Client, error) { return nil, nil }, make(chan struct{}, 1))

	m.runCommand(chat.CommandMsg{Name: cmdConfig})
	if m.settings == nil {
		t.Fatal("/settings did not open the settings screen")
	}
	view := plain(m)
	for _, want := range []string{"Settings", "Theme", "Editor", "Startup profile", "Update check"} {
		if !strings.Contains(view, want) {
			t.Errorf("settings screen missing %q", want)
		}
	}

	// Theme is the first row: turn it once (auto → dark) and the palette
	// change is applied, not just recorded.
	m.Update(arrow(tea.KeyRight))
	if m.prefs.Theme != prefs.ThemeDark {
		t.Errorf("theme = %q after one turn; want dark", m.prefs.Theme)
	}

	// Down to the system group: turning the update check off must persist.
	m.settings.Select(rowUpdates)
	m.Update(arrow(tea.KeyRight))
	if m.prefs.UpdateCheck {
		t.Error("update check still on after turning it off")
	}

	raw, err := os.ReadFile(prefs.Path(cfgPath))
	if err != nil {
		t.Fatalf("settings not written: %v", err)
	}
	if !strings.Contains(string(raw), `"theme": "dark"`) || !strings.Contains(string(raw), `"updateCheck": false`) {
		t.Errorf("settings.json = %s; want the turned values", raw)
	}

	// esc closes and hands the content region back to the tab.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.settings != nil {
		t.Error("esc did not close the settings screen")
	}
	if !strings.Contains(plain(m), "Ask something") {
		t.Error("closing the screen did not restore the Chat tab")
	}
}

// TestSettingsStartupProfileRow pins the profile pin: the row lists the
// profiles, and picking one is what the next launch starts on.
func TestSettingsStartupProfileRow(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "aibench.yaml")
	t.Setenv("CONFIG_FILE", cfgPath)
	t.Setenv("OPENAI_API_KEY", "k")

	f := config.File{Profiles: map[string]config.Profile{
		"alpha": {Kind: config.KindModel},
		"bravo": {Kind: config.KindModel},
	}}
	m := newTestModel()
	m.EnableProfiles(f, "alpha", func(config.Config) (provider.Client, error) { return nil, nil }, make(chan struct{}, 1))
	m.openSettings(rowStartup)
	m.Update(arrow(tea.KeyRight)) // last used → alpha

	if m.prefs.StartupProfile != "alpha" {
		t.Fatalf("startup = %q; want the pinned alpha", m.prefs.StartupProfile)
	}
	if got := prefs.ActiveProfile(f, prefs.Path(cfgPath)); got != "alpha" {
		t.Errorf("next launch would open %q; want the pinned alpha", got)
	}

	// Turning it back to "last used" unpins.
	m.Update(arrow(tea.KeyLeft))
	if m.prefs.StartupProfile != prefs.StartupLast {
		t.Errorf("startup = %q; want it unpinned", m.prefs.StartupProfile)
	}
}

// TestInitCommand pins /init: it scaffolds ./.aibench in the current
// folder (starter yaml + schema copy), refuses to run twice, and leaves
// the running session's config alone.
func TestInitCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CONFIG_FILE", "") // let the ordinary search see the new folder

	m := newTestModel()
	m.runCommand(chat.CommandMsg{Name: cmdInit})

	raw, err := os.ReadFile(filepath.Join(".aibench", "aibench.yaml"))
	if err != nil {
		t.Fatalf("/init did not scaffold the local config: %v", err)
	}
	if !strings.Contains(string(raw), "profiles:") {
		t.Error("scaffolded config is missing the profiles skeleton")
	}
	if !strings.Contains(string(raw), "$schema="+config.SchemaURL) {
		t.Error("scaffolded config's modeline does not reference the published schema")
	}
	if _, err := os.Stat(filepath.Join(".aibench", "internal")); !os.IsNotExist(err) {
		t.Error("/init grew a machine-owned internal/ tree; want none")
	}

	// A second /init refuses instead of overwriting.
	before, _ := os.ReadFile(filepath.Join(".aibench", "aibench.yaml"))
	m.runCommand(chat.CommandMsg{Name: cmdInit})
	after, _ := os.ReadFile(filepath.Join(".aibench", "aibench.yaml"))
	if string(before) != string(after) {
		t.Error("second /init rewrote the scaffolded config")
	}
}
