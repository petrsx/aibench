// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package e2e

// Interaction flows over a real transcript (one mock exchange), driven
// through the pump: selection, find, prompt reload, and the profile +
// prompt lifecycles.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/prefs"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/tui"
	"github.com/petrsx/aibench/internal/tui/prompt"
)

// TestDragCopySelection pins the chat selection flow: a drag shows the
// copied notice and keeps the highlight; the next click clears it.
func TestDragCopySelection(t *testing.T) {
	srv := sseChat(t, strings.Repeat("selectme ", 40))
	a := newApp(t, cfgFor(srv.URL), 100, 30)
	a.typeText("hi")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("selectme")
	row := a.row("selectme")

	// The selection background (48;5;62) must appear on the dragged
	// transcript rows — the whole frame won't do, the active tab box uses
	// the same background.
	selectedRow := func() bool {
		lines := strings.Split(a.view(), "\n")
		return row < len(lines) && strings.Contains(lines[row], "48;5;62")
	}

	a.send(tea.MouseClickMsg{X: 4, Y: row, Button: tea.MouseLeft})
	a.send(tea.MouseMotionMsg{X: 40, Y: row + 1, Button: tea.MouseLeft})
	a.send(tea.MouseReleaseMsg{X: 40, Y: row + 1, Button: tea.MouseLeft})

	if !strings.Contains(a.plain(), "copied to clipboard") {
		t.Error("drag release did not show the copied notice")
	}
	if !selectedRow() {
		t.Error("selection highlight missing on the dragged row")
	}

	a.send(tea.MouseClickMsg{X: 4, Y: row, Button: tea.MouseLeft})
	a.send(tea.MouseReleaseMsg{X: 4, Y: row, Button: tea.MouseLeft})
	if selectedRow() {
		t.Error("selection highlight should clear on the next click")
	}
}

// TestChatFind pins the shared find overlay on the chat transcript:
// ctrl+f opens it, the query renders, esc closes it without quitting.
// (The Inspector shares the same internal/tui/find widget.)
func TestChatFind(t *testing.T) {
	srv := sseChat(t, "the needle is here")
	a := newApp(t, cfgFor(srv.URL), 100, 30)
	a.typeText("hi")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("the needle is here")

	a.send(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	a.typeText("needle")
	if got := strings.Count(a.plain(), "needle"); got < 2 {
		t.Fatalf("frame shows %d×%q; want the transcript match plus the overlay query", got, "needle")
	}

	// The sole match is the current one, so exactly its transcript row wears
	// the SelectionStyle band (fg 230 on the accent background) — pinning the
	// highlight to the matched line, on styled content, where the viewport's
	// own highlight API used to light up the wrong row.
	const selSGR = "38;5;230;48;5;62"
	raw := strings.Split(a.view(), "\n")
	for i, plain := range strings.Split(a.plain(), "\n") {
		if i < 3 {
			continue // the header's active tab wears the same style
		}
		isMatchRow := strings.Contains(plain, "the needle is here")
		if lit := strings.Contains(raw[i], selSGR); lit != isMatchRow {
			t.Errorf("row %d (%q): highlight band = %v; want it on the match row only", i, strings.TrimSpace(plain), lit)
		}
	}

	a.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.quit.Load() {
		t.Fatal("esc with the find overlay open must close it, not quit")
	}
	if got := strings.Count(a.plain(), "needle"); got != 1 {
		t.Errorf("frame shows %d×%q after close; want only the transcript", got, "needle")
	}
}

// TestPromptReload pins the Prompt tab's file reload: the edited markdown
// and frontmatter params replace the loaded state on ReloadMsg (the
// fsnotify wiring itself lives in internal/preset).
func TestPromptReload(t *testing.T) {
	promptFile := filepath.Join(t.TempDir(), "prompt.md")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(promptFile, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("---\ntemperature: \"0.3\"\n---\nOld prompt body.")

	cfg := cfgFor("http://127.0.0.1:9")
	cfg.PromptFile = promptFile
	a := newApp(t, cfg, 210, 45)
	shiftTab := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	a.send(shiftTab)
	a.send(shiftTab)
	v := a.plain()
	if !strings.Contains(v, "Old prompt body.") || !strings.Contains(v, "0.3") {
		t.Fatal("prompt tab did not load the initial file")
	}

	write("---\ntemperature: \"0.9\"\n---\nNew prompt body.")
	a.send(prompt.ReloadMsg{})
	v = a.plain()
	if !strings.Contains(v, "New prompt body.") || !strings.Contains(v, "0.9") {
		t.Error("ReloadMsg did not apply the edited file")
	}
	if strings.Contains(v, "Old prompt body.") {
		t.Error("stale prompt body survived the reload")
	}
}

// TestProfileSwitchAndReload pins the profile lifecycle: ctrl+s switches
// to the next profile, persists the selection into the state file (never
// the config), and a config-file change signal re-resolves the active
// profile in place.
func TestProfileSwitchAndReload(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "aibench.yaml")
	writeCfg := func(bravoModel string) {
		t.Helper()
		yaml := "# keep this comment\nprofiles:\n" +
			"  alpha:\n    kind: model\n    api:\n      provider: openai\n      base: http://127.0.0.1:9\n      model: alpha-model\n    auth:\n      credentials: env\n" +
			"  bravo:\n    kind: model\n    api:\n      provider: openai\n      base: http://127.0.0.1:9\n      model: " + bravoModel + "\n    auth:\n      credentials: env\n"
		if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeCfg("bravo-model")
	t.Setenv("CONFIG_FILE", cfgPath)
	t.Setenv("OPENAI_API_KEY", "test-key")

	f, ok, err := config.LoadFile(cfgPath)
	if err != nil || !ok {
		t.Fatalf("LoadFile: ok=%v err=%v", ok, err)
	}
	active := prefs.ActiveProfile(f, prefs.Path(cfgPath))
	if active != "alpha" {
		t.Fatalf("initial active = %q; want first-by-name alpha", active)
	}
	cfg, err := f.Resolve(active)
	if err != nil {
		t.Fatal(err)
	}

	configCh := make(chan struct{}, 1)
	a := newApp(t, cfg, 100, 30, func(m *tui.Model) {
		m.EnableProfiles(f, active, func(c config.Config) (provider.Client, error) {
			return provider.NewClient(c, make(chan capture.Event, 8))
		}, configCh)
	})
	if !strings.Contains(a.plain(), "alpha-model") {
		t.Fatal("initial frame does not show the alpha profile's model")
	}

	// /profiles menu: filter to bravo, enter switches; selection persisted
	// to the app's settings file only.
	a.typeText("/profiles bravo")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("bravo-model")
	settings, err := os.ReadFile(prefs.Path(cfgPath))
	if err != nil || !strings.Contains(string(settings), `"activeProfile": "bravo"`) {
		t.Errorf("settings file = %q, %v; want activeProfile bravo persisted", settings, err)
	}
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "# keep this comment") || strings.Contains(string(raw), "activeProfile") {
		t.Error("the human config must never be rewritten")
	}

	// Config hot-reload: the watcher signal re-resolves the active
	// profile in place (the fsnotify → channel wiring lives in preset).
	writeCfg("charlie")
	configCh <- struct{}{}
	a.waitText("charlie")
}

// TestTurnEndNotification pins the unfocused turn-end announcement: a
// reply completing while the window is blurred emits the terminal
// notification; a focused window stays silent.
func TestTurnEndNotification(t *testing.T) {
	srv := sseChat(t, "done and dusted")
	a := newApp(t, cfgFor(srv.URL), 100, 30)

	a.typeText("hi")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Blur before pumping: the deltas only apply once waitFrame drains
	// the queue, so the completion deterministically lands unfocused.
	a.send(tea.BlurMsg{})
	a.waitFrame(func(p string) bool {
		return strings.Contains(p, "done and dusted") && a.bell.Load()
	})

	// Focused: the same flow must not ring.
	b := newApp(t, cfgFor(srv.URL), 100, 30)
	b.typeText("hi")
	b.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	b.waitFrame(func(p string) bool {
		return strings.Contains(p, "done and dusted") && !strings.ContainsAny(p, "✢✳✶✻✽")
	})
	if b.bell.Load() {
		t.Error("focused completion must not emit the terminal notification")
	}
}

// TestProfileMenu pins /profiles' inline options: typing the command
// lists every profile under the composer, the filter narrows it, enter
// switches, esc dismisses without a switch.
func TestProfileMenu(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "aibench.yaml")
	yaml := "profiles:\n" +
		"  alpha:\n    kind: model\n    api:\n      provider: openai\n      base: http://127.0.0.1:9\n      model: alpha-model\n    auth:\n      credentials: env\n" +
		"  bravo:\n    kind: model\n    api:\n      provider: openai\n      base: http://127.0.0.1:9\n      model: bravo-model\n    auth:\n      credentials: env\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", cfgPath)
	t.Setenv("OPENAI_API_KEY", "test-key")
	f, _, err := config.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := f.Resolve("alpha")
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(t, cfg, 100, 30, func(m *tui.Model) {
		m.EnableProfiles(f, "alpha", func(c config.Config) (provider.Client, error) {
			return provider.NewClient(c, make(chan capture.Event, 8))
		}, make(chan struct{}, 1))
	})

	// The options stage lists both profiles, the active one marked.
	a.typeText("/profiles ")
	v := a.plain()
	if !strings.Contains(v, "alpha ✔") || !strings.Contains(v, "bravo-model · model") {
		t.Fatal("/profiles did not list the profiles inline")
	}

	// Esc dismisses the menu (clears the draft) without switching.
	a.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if strings.Contains(a.plain(), "bravo-model · model") {
		t.Fatal("esc did not dismiss the menu")
	}
	if a.quit.Load() {
		t.Fatal("esc on the menu must not count toward quitting")
	}

	// Filter + enter switches.
	a.typeText("/profiles brav")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("bravo-model")
}

// TestStartersMenu pins /starters' inline options: each starter file is
// one script, listed by name; typing filters, enter runs the pick.
func TestStartersMenu(t *testing.T) {
	dir := t.TempDir()
	one := filepath.Join(dir, "smoke.starters.md")
	two := filepath.Join(dir, "edge-cases.starters.md")
	if err := os.WriteFile(one, []byte("## hello\nsay hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(two, []byte("## typo weather\nwhats the weather in pariis\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := cfgFor("http://127.0.0.1:9")
	cfg.StartersFiles = []string{one, two}
	a := newApp(t, cfg, 100, 30)
	a.waitText("Run starter script") // the launch offer
	a.send(tea.KeyPressMsg{Code: tea.KeyEscape})

	a.typeText("/starters ")
	v := a.plain()
	if !strings.Contains(v, "smoke") || !strings.Contains(v, "edge-cases") {
		t.Fatal("/starters did not list both scripts")
	}
	a.typeText("edge")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	// The picked script runs: its prompt goes out and the unroutable
	// endpoint stops it.
	a.waitText("whats the weather in pariis", "script stopped on error")
}

// TestScriptPlayback pins the TUI script run (the `send --script` twin):
// the picker's "run script" entry sends every starter in order, each
// after the previous reply lands, and reports completion.
func TestScriptPlayback(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := posts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, `data: {"id":"%d","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"reply number %d"}}]}`+"\n\n", n, n)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	starters := filepath.Join(t.TempDir(), "canned.starters.md")
	body := "## first\nsend me first\n\n## second\nsend me second\n"
	if err := os.WriteFile(starters, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := cfgFor(srv.URL)
	cfg.StartersFiles = []string{starters}
	a := newApp(t, cfg, 100, 30)

	// The launch offer, accepted: enter runs the highlighted script —
	// both prompts and both replies land in order, each sent only after
	// the previous reply — then the done notice (named for the script).
	a.waitText("Run starter script")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("send me first", "reply number 1", "send me second", "reply number 2", "canned done")
	if posts.Load() != 2 {
		t.Errorf("model calls = %d; want one per starter", posts.Load())
	}
	v := a.plain()
	if strings.Index(v, "send me first") > strings.Index(v, "send me second") {
		t.Error("script order lost in the transcript")
	}

	// /clear then /starters replays it manually.
	a.typeText("/clear")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitFrame(func(p string) bool { // the transcript cleared, no banner replaces it
		return !strings.Contains(p, "send me first")
	})
	a.typeText("/starters ")
	if !strings.Contains(a.plain(), "canned") {
		t.Fatal("/starters is missing the script")
	}
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("reply number 4", "canned done")
	if posts.Load() != 4 {
		t.Errorf("model calls after replay = %d; want two runs of two", posts.Load())
	}
}

// TestLaunchWithUnusableProfile pins the launch contract: an active
// profile whose credentials don't resolve must not keep the app off the
// screen. The shell starts client-less, reports the problem, refuses
// sends with a message instead of crashing, and still switches to a
// profile that works — the whole point of a tool for poking at endpoints.
func TestLaunchWithUnusableProfile(t *testing.T) {
	dir := t.TempDir()
	// The broken profile's credentials file defines nothing usable; the
	// process env must not quietly supply one either.
	if err := os.WriteFile(filepath.Join(dir, "broken.env"), []byte("UNUSED=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "good.env"), []byte("OPENAI_API_KEY=sk-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	yaml := "profiles:\n" +
		"  alpha:\n    kind: model\n    api:\n      provider: openai\n      base: http://127.0.0.1:9\n      model: alpha-model\n    auth:\n      credentials: broken.env\n" +
		"  bravo:\n    kind: model\n    api:\n      provider: openai\n      base: http://127.0.0.1:9\n      model: bravo-model\n    auth:\n      credentials: good.env\n"
	cfgPath := filepath.Join(dir, "aibench.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	// Credentials resolve against the working directory like every other
	// path in the config, so the test stands where its .env files are.
	t.Chdir(dir)
	t.Setenv("CONFIG_FILE", cfgPath)
	for _, k := range []string{"OPENAI_API_KEY", "AZURE_TOKEN", "AZURE_USE_LOGIN", "AZURE_TENANT_ID", "AZURE_CLIENT_ID", "AZURE_CLIENT_SECRET"} {
		t.Setenv(k, "")
	}

	f, ok, err := config.LoadFile(cfgPath)
	if err != nil || !ok {
		t.Fatalf("LoadFile: ok=%v err=%v", ok, err)
	}
	// The launch itself resolves nothing — alpha's credentials are read
	// only when the shell selects it on Init.
	a := newClientlessApp(t, 100, 30, func(m *tui.Model) {
		m.EnableProfiles(f, "alpha", func(c config.Config) (provider.Client, error) {
			return provider.NewClient(c, make(chan capture.Event, 8))
		}, make(chan struct{}, 1))
	})

	// It rendered at all — that is the regression this guards.
	a.waitText("Ask something")
	a.waitFrame(func(plain string) bool { return strings.Contains(plain, "no credentials") })

	// Sending is refused with a message, not a panic on a nil client.
	a.typeText("hello")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitFrame(func(plain string) bool { return strings.Contains(plain, "no endpoint") })

	// And the working profile is one pick away.
	a.typeText("/profiles bravo")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitFrame(func(plain string) bool { return strings.Contains(plain, "bravo-model") })
}

// TestPromptSetFollowsTheProfile pins where a prompt set comes from: the
// profile that names it. Switching profile swaps the instructions, the
// header names the new set, and the conversation clears — there is no
// prompt command, because choosing an endpoint is choosing what it is
// being tested with.
func TestPromptSetFollowsTheProfile(t *testing.T) {
	srv := sseChat(t, "an answer")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "aibench.yaml")
	for name, body := range map[string]string{
		"weather.md": "Weather instructions body.",
		"pirate.md":  "Pirate instructions body.",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	yaml := "prompts:\n" +
		"  weather:\n    instructions: weather.md\n" +
		"  pirate:\n    instructions: pirate.md\n" +
		"profiles:\n" +
		"  alpha:\n    kind: model\n    api:\n      provider: openai\n      base: " + srv.URL + "\n      model: alpha-model\n    auth:\n      credentials: env\n    prompt: weather\n" +
		"  bravo:\n    kind: model\n    api:\n      provider: openai\n      base: " + srv.URL + "\n      model: bravo-model\n    auth:\n      credentials: env\n    prompt: pirate\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	// A set's instructions resolve against the working directory, so the
	// test stands where the files are — which is what a user does when
	// the path in their config says `weather.md`.
	t.Chdir(dir)
	t.Setenv("CONFIG_FILE", cfgPath)
	t.Setenv("OPENAI_API_KEY", "test-key")
	f, _, err := config.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := f.Resolve("alpha")
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(t, cfg, 120, 35, func(m *tui.Model) {
		m.EnableProfiles(f, "alpha", func(c config.Config) (provider.Client, error) {
			return provider.NewClient(c, make(chan capture.Event, 8))
		}, make(chan struct{}, 1))
	})
	// The pin lands: the Metrics identity strip names the set, a
	// conversation accumulates.
	a.waitText("weather")
	a.typeText("cleared by the switch")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("an answer")

	// No command of its own: /prompt is gone from the menu, and typing it
	// is an ordinary message rather than a switch.
	a.typeText("/")
	if v := a.plain(); strings.Contains(v, "switch prompt set") {
		t.Error("the menu still offers a prompt command")
	}
	a.send(tea.KeyPressMsg{Code: tea.KeyBackspace})

	// The profile carries the set: switching to bravo brings pirate with
	// it and clears the conversation.
	a.typeText("/profiles bravo")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitFrame(func(p string) bool {
		return !strings.Contains(p, "cleared by the switch") && strings.Contains(p, "pirate")
	})

	// The Prompt tab shows the new set's instructions.
	shiftTab := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	a.send(shiftTab)
	a.send(shiftTab)
	a.waitText("Pirate instructions body.")
}
