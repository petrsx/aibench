// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package prefs

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/petrsx/aibench/internal/config"
)

// TestSettingsDefaults pins what a fresh install behaves like: no file
// is not an error, and a file naming one key leaves the rest alone.
func TestSettingsDefaults(t *testing.T) {
	path := Path(filepath.Join(t.TempDir(), "aibench.yaml"))

	got := Load(path)
	want := Defaults()
	if got != want {
		t.Errorf("no file: Load = %+v; want the defaults %+v", got, want)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got = Load(path)
	if got.Theme != ThemeDark {
		t.Errorf("theme = %q; want dark", got.Theme)
	}
	if !got.UpdateCheck || got.StartupProfile != StartupLast {
		t.Errorf("partial file dropped the other defaults: %+v", got)
	}

	// Junk in, defaults out — a preferences file must never stop the app.
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got != Defaults() {
		t.Errorf("malformed file: Load = %+v; want the defaults", got)
	}
	// So must a value outside the vocabulary.
	if err := os.WriteFile(path, []byte(`{"theme":"neon"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(path).Theme; got != ThemeAuto {
		t.Errorf("unknown theme = %q; want auto", got)
	}
}

// TestSaveSettings pins the round trip and the file's shape: it is meant
// to be read and hand-edited, so it stays indented JSON.
func TestSaveSettings(t *testing.T) {
	path := Path(filepath.Join(t.TempDir(), "aibench.yaml"))
	want := Defaults()
	want.Editor = "code -w"
	want.Theme = ThemeLight
	want.UpdateCheck = false
	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if got := Load(path); got != want {
		t.Errorf("round trip = %+v; want %+v", got, want)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "\n  \"editor\": \"code -w\"") || !strings.HasSuffix(string(raw), "\n") {
		t.Errorf("settings file is not hand-editable JSON:\n%s", raw)
	}
}

// TestActiveProfile pins the startup pick: a pin that resolves wins, the
// last-used one is the fallback, and neither surviving means the first
// profile by name.
func TestActiveProfile(t *testing.T) {
	f := config.File{Profiles: map[string]config.Profile{
		"bravo": {Kind: config.KindModel},
		"alpha": {Kind: config.KindModel},
	}}
	path := Path(filepath.Join(t.TempDir(), "aibench.yaml"))

	if got := ActiveProfile(f, path); got != "alpha" {
		t.Errorf("no settings file: ActiveProfile = %q; want first-by-name alpha", got)
	}

	if err := SaveActiveProfile(path, "bravo"); err != nil {
		t.Fatalf("SaveActiveProfile() error = %v", err)
	}
	if got := ActiveProfile(f, path); got != "bravo" {
		t.Errorf("last used: ActiveProfile = %q; want bravo", got)
	}

	// A pin outranks the last-used one.
	s := Load(path)
	s.StartupProfile = "alpha"
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	if got := ActiveProfile(f, path); got != "alpha" {
		t.Errorf("pinned: ActiveProfile = %q; want alpha", got)
	}

	// A pin naming a profile that no longer exists falls through to the
	// last-used one rather than failing.
	s.StartupProfile = "gone"
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	if got := ActiveProfile(f, path); got != "bravo" {
		t.Errorf("stale pin: ActiveProfile = %q; want the last-used bravo", got)
	}
	if got := ActiveProfile(config.File{}, path); got != "" {
		t.Errorf("no profiles: ActiveProfile = %q; want empty", got)
	}
}

// TestSaveActiveProfileKeepsSettings pins that the bookkeeping write —
// one per profile switch — leaves the user's choices alone.
func TestSaveActiveProfileKeepsSettings(t *testing.T) {
	path := Path(filepath.Join(t.TempDir(), "aibench.yaml"))
	s := Defaults()
	s.Editor = "nvim"
	s.UpdateCheck = false
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	if err := SaveActiveProfile(path, "dev"); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	if got.Editor != "nvim" || got.UpdateCheck || got.ActiveProfile != "dev" {
		t.Errorf("after SaveActiveProfile: %+v; want the editor and switch kept", got)
	}
}

func TestEditorCommand(t *testing.T) {
	tests := []struct {
		name                 string
		pref, visual, editor string
		want                 []string
	}{
		{"nothing declared", "", "", "", nil},
		{"EDITOR", "", "", "nano", []string{"nano"}},
		{"VISUAL wins over EDITOR", "", "hx", "nano", []string{"hx"}},
		{"the preference wins over both", "vim", "hx", "nano", []string{"vim"}},
		{"flags split into argv", "", "", "code -w", []string{"code", "-w"}},
		{"blank is not a declaration", "  ", "   ", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("VISUAL", tt.visual)
			t.Setenv("EDITOR", tt.editor)
			if got := EditorCommand(tt.pref); !slices.Equal(got, tt.want) {
				t.Errorf("EditorCommand(%q) = %v; want %v", tt.pref, got, tt.want)
			}
		})
	}
}
