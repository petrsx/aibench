// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/provider"
)

func TestDetaches(t *testing.T) {
	tests := []struct {
		argv []string
		want bool
	}{
		{[]string{"code"}, true},        // GUI: run beside the TUI
		{[]string{"code", "-w"}, false}, // explicit wait: the user asked to block
		{[]string{"code", "--wait"}, false},
		{[]string{"vim"}, false},               // terminal: needs the terminal
		{[]string{"open", "-t"}, false},        // unknown: the safe suspend path
		{[]string{"/usr/local/bin/zed"}, true}, // paths resolve by basename
	}
	for _, tt := range tests {
		if got := detaches(tt.argv); got != tt.want {
			t.Errorf("detaches(%v) = %v; want %v", tt.argv, got, tt.want)
		}
	}
}

// TestEditorFromSettings pins the two ctrl+e paths: with nothing
// declared it opens the settings screen on the Editor row instead of
// dropping the user into whatever the platform fallback is, and once the
// setting names an editor it goes straight there.
func TestEditorFromSettings(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "aibench.yaml")
	t.Setenv("CONFIG_FILE", cfgPath)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	m := newTestModel()
	m.EnableProfiles(config.File{Profiles: map[string]config.Profile{"dev": {Kind: config.KindModel}}},
		"dev", func(config.Config) (provider.Client, error) { return nil, nil }, make(chan struct{}, 1))

	if cmd := m.editConfig(); cmd == nil {
		t.Error("nothing declared: ctrl+e said nothing")
	}
	if m.settings == nil {
		t.Fatal("nothing declared: ctrl+e did not open the settings screen")
	}
	if got := m.settings.Focused().Key; got != rowEditor {
		t.Errorf("settings opened on row %q; want the editor row", got)
	}

	// Declared: straight to the editor, no detour.
	m.settings = nil
	m.prefs.Editor = "nano"
	if cmd := m.editConfig(); cmd == nil {
		t.Error("declared editor: ctrl+e did not run it")
	}
	if m.settings != nil {
		t.Error("declared editor: ctrl+e opened the settings screen anyway")
	}
}

// TestReloadConfig covers what ctrl+e comes back to (and what the file
// watcher signals): the edited file is re-read and the active profile
// re-resolved in place.
func TestReloadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aibench.yaml")
	write := func(model string) {
		t.Helper()
		body := "profiles:\n  dev:\n    kind: model\n    api:\n      provider: openai\n" +
			"      base: https://dev.example.net\n      model: " + model + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("gpt-5.4-mini")
	t.Setenv("CONFIG_FILE", path)
	t.Setenv("OPENAI_API_KEY", "k")

	f, ok, err := config.LoadFile(path)
	if err != nil || !ok {
		t.Fatalf("LoadFile() = %v, %v", ok, err)
	}
	m := newTestModel()
	m.EnableProfiles(f, "dev", func(config.Config) (provider.Client, error) { return nil, nil }, make(chan struct{}, 1))
	m.switchProfile("dev")

	write("gpt-5.4")
	m.reloadConfig()
	if m.cfg.Model != "gpt-5.4" {
		t.Errorf("model = %q after reload; want the edited gpt-5.4", m.cfg.Model)
	}

	// A file that stopped parsing keeps the running profile.
	if err := os.WriteFile(path, []byte("profiles: [oops\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.reloadConfig()
	if m.cfg.Model != "gpt-5.4" || m.activeProfile != "dev" {
		t.Errorf("broken config changed the live profile: %q / %q", m.activeProfile, m.cfg.Model)
	}
}
