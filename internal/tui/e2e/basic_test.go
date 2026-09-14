// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/tui"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// Quitting runs through a real headless tea.Program (teatest) — the one
// place the full program loop and renderer are exercised.

func TestExitWordQuits(t *testing.T) {
	tm := teatest.NewTestModel(t, newModel(t, cfgFor("http://127.0.0.1:9")),
		teatest.WithInitialTermSize(210, 45))
	tm.Type("exit")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}

// TestTinyTerminal pins the layout clamp: a 4x3 terminal must not panic,
// and a resize recovers the full layout.
func TestTinyTerminal(t *testing.T) {
	a := newApp(t, cfgFor("http://127.0.0.1:9"), 4, 3)
	_ = a.view() // must not panic
	a.send(tea.WindowSizeMsg{Width: 210, Height: 45})
	if !strings.Contains(a.plain(), "Ask something") {
		t.Error("resize did not recover the composer")
	}
}

// TestTabCycling pins shift+tab: Chat → Inspector → Prompt → Chat, with
// the Prompt tab showing the rendered prompt and the chat api's params.
func TestTabCycling(t *testing.T) {
	promptFile := filepath.Join(t.TempDir(), "prompt.md")
	// Frontmatter params: the Prompt tab lists what the file defines.
	if err := os.WriteFile(promptFile,
		[]byte("---\ntemperature: 0.4\n---\n\nBe terse about trains."), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := cfgFor("http://127.0.0.1:9")
	cfg.PromptFile = promptFile
	a := newApp(t, cfg, 210, 45)
	shiftTab := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}

	a.send(shiftTab)
	if !strings.Contains(a.plain(), "request/response") {
		t.Fatal("first shift+tab did not reach the Inspector")
	}
	a.send(shiftTab)
	v := a.plain()
	if !strings.Contains(v, "temperature") || !strings.Contains(v, "Be terse about trains.") {
		t.Fatal("second shift+tab did not reach the Prompt tab with the prompt rendered")
	}
	// Only what the file defines: a knob the api has but the file does
	// not set is not a row.
	if strings.Contains(v, "seed") {
		t.Error("the Params panel lists a param the prompt file never set")
	}
	if strings.Contains(v, "top_k") { // anthropic-only knob absent
		t.Error("anthropic-only param top_k leaked into the chat api's Prompt tab")
	}
	a.send(shiftTab)
	if !strings.Contains(a.plain(), "Ask something") {
		t.Error("third shift+tab did not return to Chat")
	}
}

// TestTabClick pins the tab-bar mouse path: a click inside the Inspector
// label (located on the rendered tab bar, not hardcoded) switches to it.
func TestTabClick(t *testing.T) {
	a := newApp(t, cfgFor("http://127.0.0.1:9"), 210, 45)
	bar := []rune(strings.Split(a.plain(), "\n")[1])
	x := strings.Index(string(bar), "Inspector")
	if x < 0 {
		t.Fatal("Inspector label not on the tab bar row")
	}
	// Byte offset → rune column: the logo glyphs before the labels are
	// multi-byte but single-width.
	col := len([]rune(string([]byte(string(bar))[:x])))
	a.send(tea.MouseClickMsg{X: col + 1, Y: 1, Button: tea.MouseLeft})
	if !strings.Contains(a.plain(), "request/response") {
		t.Error("clicking the Inspector label did not switch to it")
	}
}

// TestAltEnterNewline pins the newline chord: both lines land in the
// growing input and nothing is sent.
func TestAltEnterNewline(t *testing.T) {
	a := newApp(t, cfgFor("http://127.0.0.1:9"), 100, 30)
	a.typeText("lineone")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	a.typeText("linetwo")
	v := a.plain()
	if !strings.Contains(v, "lineone") || !strings.Contains(v, "linetwo") {
		t.Error("both lines should show in the multi-line input")
	}
	if strings.Contains(v, "chat/completions") {
		t.Error("alt+enter must not send")
	}
}

// TestStartersLaunchOffer pins the starters startup contract: declaring
// starters opens the /starters selector at launch — an offer, never a
// send. Esc dismisses with nothing sent; enter runs the highlighted
// script; ↑ recalls a prompt into the composer.
func TestStartersLaunchOffer(t *testing.T) {
	starters := filepath.Join(t.TempDir(), "canned.starters.md")
	if err := os.WriteFile(starters, []byte("## canned\npredefined hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := cfgFor("http://127.0.0.1:9")
	cfg.StartersFiles = []string{starters}
	a := newApp(t, cfg, 100, 30)

	// The selector is open on the script, and nothing has been sent.
	a.waitText("Run starter script", "canned")
	if strings.Contains(a.plain(), "predefined hello") {
		t.Fatal("launch sent a starter; the selector is an offer only")
	}

	// Esc dismisses: empty composer, and the empty transcript stays clean —
	// no starter banner (launch already made the offer; /starters re-opens it).
	a.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if strings.Contains(a.plain(), "Run starter script") {
		t.Error("esc did not dismiss the launch selector")
	}
	if strings.Contains(a.plain(), "canned") {
		t.Error("the empty transcript must stay clean of starter titles")
	}

	// Running it: the send travels like typed input, and the unroutable
	// endpoint stops the script with an error notice.
	a.typeText("/starters")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter}) // into the options
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter}) // run "canned"
	a.waitText("predefined hello", "script stopped on error")

	// The composer is empty again after the run — ↑ recalls the prompt
	// into it (the text then shows twice: the sent turn + the draft).
	a.send(tea.KeyPressMsg{Code: tea.KeyUp})
	if strings.Count(a.plain(), "predefined hello") < 2 {
		t.Error("Up did not recall the starter prompt into the composer")
	}
}

// TestGoldenInitialFrame pins the composed initial frame (header, tab
// boxes, empty transcript, composer, Tokens + Metrics panes) at a fixed
// size. Regenerate with `go test ./internal/tui/e2e -update` and review
// the diff like any code change.
func TestGoldenInitialFrame(t *testing.T) {
	a := newApp(t, cfgFor("https://gw.example.net/v1"), 120, 35)
	goldenColumns(t, a.view())
}

// withProfiles arms in-app profile switching on a fresh shell, the way
// the launch path does — two profiles, so the pickers have rows.
func withProfiles(t *testing.T) func(*tui.Model) {
	t.Helper()
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "aibench.yaml"))
	t.Setenv("OPENAI_API_KEY", "test-key")
	f := config.File{Profiles: map[string]config.Profile{
		"alpha": {Kind: config.KindModel, API: config.ProfileAPI{Provider: "openai", Base: "https://gw.example.net/v1", Model: "alpha-model"}},
		"bravo": {Kind: config.KindModel, API: config.ProfileAPI{Provider: "openai", Base: "https://gw.example.net/v1", Model: "bravo-model"}},
	}}
	return func(m *tui.Model) {
		m.EnableProfiles(f, "alpha", func(c config.Config) (provider.Client, error) {
			return provider.NewClient(c, make(chan capture.Event, 8))
		}, make(chan struct{}, 1))
	}
}

// TestGoldenProfilePickerFrame pins /profiles' inline options stage.
func TestGoldenProfilePickerFrame(t *testing.T) {
	// No starters here: they would auto-run against the example endpoint
	// and fill the frame with nondeterministic error text.
	a := newApp(t, cfgFor("https://gw.example.net/v1"), 120, 35, withProfiles(t))
	a.typeText("/profiles ")
	goldenColumns(t, a.view())
}

// TestSettingsScreen pins /settings: the app's own preferences take over
// the content region, arrows turn a value, esc hands the tab back.
func TestSettingsScreen(t *testing.T) {
	// Turning the theme row repaints the shared palette; put it back so
	// the golden frames after this test render in the package default.
	t.Cleanup(func() { theme.Apply(true) })
	a := newApp(t, cfgFor("http://127.0.0.1:9"), 120, 35, withProfiles(t))
	a.typeText("/settings")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("Update check")
	v := a.plain()
	for _, want := range []string{"Settings", "Theme", "auto", "Editor", "Startup profile", "Update check", "↑/↓", "esc"} {
		if !strings.Contains(v, want) {
			t.Errorf("settings screen missing %q", want)
		}
	}
	a.send(tea.KeyPressMsg{Code: tea.KeyRight}) // theme: auto → dark
	if !strings.Contains(a.plain(), "dark") {
		t.Error("turning the theme row did not show the new value")
	}
	a.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !strings.Contains(a.plain(), "Ask something") {
		t.Error("esc did not hand the content region back to the tab")
	}
}

// TestGoldenCommandMenuFrame pins the composer's open command menu.
func TestGoldenCommandMenuFrame(t *testing.T) {
	a := newApp(t, cfgFor("https://gw.example.net/v1"), 120, 35, withProfiles(t))
	a.typeText("/")
	goldenColumns(t, a.view())
}

// TestGoldenSettingsFrame pins the settings screen.
func TestGoldenSettingsFrame(t *testing.T) {
	a := newApp(t, cfgFor("https://gw.example.net/v1"), 120, 35, withProfiles(t))
	a.typeText("/settings")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("Update check")
	goldenScreen(t, a.view())
}

// TestSlashCommands pins the composer's command menu: "/" opens it,
// typing filters, tab completes the selected name, and enter runs it.
func TestSlashCommands(t *testing.T) {
	a := newApp(t, cfgFor("http://127.0.0.1:9"), 120, 35, withProfiles(t))

	a.typeText("/")
	v := a.plain()
	for _, want := range []string{"/settings", "/profiles", "/profiles-edit", "/clear", "/help", "/exit"} {
		if !strings.Contains(v, want) {
			t.Errorf("command menu missing %q", want)
		}
	}

	a.typeText("se")
	v = a.plain()
	if !strings.Contains(v, "/settings") || strings.Contains(v, "/exit") {
		t.Error("typing did not filter the menu down to /settings")
	}

	a.send(tea.KeyPressMsg{Code: tea.KeyTab})
	if !strings.Contains(a.plain(), "/settings ") {
		t.Error("tab did not complete the command into the composer")
	}

	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("Update check") // the command round-trips through the shell

	// esc closes it, and an ordinary path-looking message still sends as
	// a message — only names the menu offered are commands.
	a.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	a.typeText("/etc/hosts")
	if strings.Contains(a.plain(), "clear the conversation") {
		t.Error("an unknown /path opened the command menu")
	}
}

// TestKeySheet pins /help's overview: the command registry, the menu
// keys, and every tab's bindings are on the sheet — and esc closes it
// without counting toward quitting.
func TestKeySheet(t *testing.T) {
	a := newApp(t, cfgFor("http://127.0.0.1:9"), 120, 40)
	a.typeText("/help")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("esc closes") // the screen's tab bar
	v := a.plain()
	for _, want := range []string{
		"Settings", "Help", // the screen's inner tabs
		"commands", "command menu", "global", "chat", "prompt", "inspector", "find", // the groups
		"/settings", "/clear", "/help", "/exit", // the registry rows
		"ctrl+c",   // the quit chord
		"complete", // a menu key
		// The bottom row of each column: the screen clips what does not
		// fit (MaxHeight), so a sheet that grew past the terminal would
		// lose these silently rather than complain.
		"close find", "latest",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("key sheet missing %q", want)
		}
	}

	// tab flips to the Settings tab of the same screen and back.
	a.send(tea.KeyPressMsg{Code: tea.KeyTab})
	if !strings.Contains(a.plain(), "Update check") {
		t.Error("tab did not switch the screen to Settings")
	}
	a.send(tea.KeyPressMsg{Code: tea.KeyTab})
	if !strings.Contains(a.plain(), "command menu") {
		t.Error("tab did not switch back to Help")
	}

	a.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if strings.Contains(a.plain(), "esc closes") {
		t.Error("esc did not close the screen")
	}
	if a.quit.Load() {
		t.Error("esc on the screen must not count toward quitting")
	}
}

// TestGoldenKeySheet pins the rendered Help tab.
func TestGoldenKeySheet(t *testing.T) {
	a := newApp(t, cfgFor("https://gw.example.net/v1"), 120, 40)
	a.typeText("/help")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("esc closes")
	goldenScreen(t, a.view())
}
