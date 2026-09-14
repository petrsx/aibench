// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/store"
	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/update"
)

func newTestModel() *Model {
	m := New(config.Config{Model: "test-model", PromptFile: "/nonexistent/prompt.md"}, nil, make(chan capture.Event, 1), make(chan struct{}, 1))
	m.width, m.height = 120, 40
	m.layout()
	m.ready = true
	return m
}

// quits reports whether the command returned by Update produces tea.QuitMsg.
func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// TestDevEscQuitIsOffByDefault pins the shipped behaviour: esc closes what
// is open and never ends the session, however many times it is pressed.
func TestDevEscQuitIsOffByDefault(t *testing.T) {
	m := newTestModel()
	for i := range 3 {
		if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); quits(cmd) {
			t.Fatalf("esc #%d quit a normal build", i+1)
		}
	}
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Mod: tea.ModAlt}); quits(cmd) {
		t.Error("the coalesced alt+esc quit a normal build")
	}
}

// TestDevEscQuits pins the developer affordance: with AIBENCH_DEV set, a
// second esc inside the window leaves.
func TestDevEscQuits(t *testing.T) {
	t.Setenv(devEnv, "1")
	m := newTestModel()

	// Don't execute the first command: it's a tea.Tick that sleeps out the
	// very window under test. Assert on state instead.
	before := m.lastEsc
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.lastEsc.After(before) {
		t.Fatal("first esc did not arm the quit window")
	}
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); !quits(cmd) {
		t.Error("second esc within the window did not quit")
	}
}

// TestDevEscWindowExpires pins the window's edge: past it, esc re-arms
// rather than quitting.
func TestDevEscWindowExpires(t *testing.T) {
	t.Setenv(devEnv, "1")
	m := newTestModel()
	m.lastEsc = time.Now().Add(-2 * escWindow)
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); quits(cmd) {
		t.Error("esc after an expired window quit; want a re-armed window")
	}
}

// TestDevModeRejectsNonBoolean pins the knob's contract: a value that is
// not a boolean leaves the affordances off rather than half on.
func TestDevModeRejectsNonBoolean(t *testing.T) {
	t.Setenv(devEnv, "yes please")
	if devMode() {
		t.Error("a non-boolean AIBENCH_DEV turned the affordances on")
	}
}

// The request lifecycle (record history, streamed-turn lifecycle, interrupt
// handling) moved to internal/state; those tests live in state_test.go.

func TestSwitchProfile(t *testing.T) {
	// Pin the config location: switchProfile persists the selection into
	// the sibling state file, which must land in the temp dir, not the cwd.
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "aibench.yaml"))
	m := newTestModel()
	m.EnableProfiles(config.File{
		Profiles: map[string]config.Profile{
			"azure-dev": {Kind: config.KindModel, API: config.ProfileAPI{Provider: "openai", Base: "https://dev.example.net", Model: "gpt-5.4-mini"}},
			"claude":    {Kind: config.KindModel, API: config.ProfileAPI{Provider: "anthropic", Base: "https://api.anthropic.com", Model: "claude-opus-4-8"}},
		},
	}, "azure-dev", func(c config.Config) (provider.Client, error) { return nil, nil }, make(chan struct{}, 1))
	t.Setenv("OPENAI_API_KEY", "k")

	m.state.AppendTurn(provider.RoleUser, "cleared by the switch", store.Complete)

	m.switchProfile("claude")
	if m.activeProfile != "claude" || m.cfg.API != config.APIMessages {
		t.Fatalf("active = %q api = %q; want claude/messages", m.activeProfile, m.cfg.API)
	}
	// The Prompt tab rebuilding its inputs for the new api is covered by the
	// prompt package (TestSetProfileRebuildsInputs).
	// A different profile clears the conversation (/clear semantics): its
	// own prompt takes over, and context from the old one would test
	// neither.
	if len(m.state.Turns()) != 0 {
		t.Error("switch kept the old conversation; want it cleared")
	}

	// A re-resolve of the profile already active (the config hot-reload
	// path) keeps the conversation — the endpoint did not change.
	m.state.AppendTurn(provider.RoleUser, "kept on re-resolve", store.Complete)
	m.switchProfile("claude")
	if len(m.state.Turns()) != 1 {
		t.Error("re-resolving the active profile cleared the conversation; want it kept")
	}

	// Unknown profile: keep the current one (the error shows on the Chat tab).
	m.switchProfile("nope")
	if m.activeProfile != "claude" {
		t.Errorf("active = %q after bad switch; want claude kept", m.activeProfile)
	}
}

// TestTipTickTurnsTheFrontTabOnly pins the hint rotation: the shell owns
// the cadence and hands it to the tab in front. A tab nobody is looking
// at must not advance — a reader switching to it would land mid-cycle,
// on whichever hint the clock happened to reach.
func TestTipTickTurnsTheFrontTabOnly(t *testing.T) {
	m := newTestModel()
	front, behind := m.chat.Tip(), m.inspector.Tip()

	_, cmd := m.Update(tipTickMsg{})
	if cmd == nil {
		t.Fatal("the tick did not re-arm itself; the rotation stops after one turn")
	}
	if m.chat.Tip() == front {
		t.Error("the front tab's hint did not turn over")
	}
	if got := m.inspector.Tip(); got != behind {
		t.Errorf("a tab out of view turned its hint over: %q → %q", behind, got)
	}
	// And it is the shown hint that the header draws.
	head, _ := renderHeader(m.width, m.activeTab, m.tabNames(), m.tabs[m.activeTab].model.Tip(), "")
	if !strings.Contains(ansi.Strip(head), m.chat.Tip()) {
		t.Errorf("the header does not carry the front tab's hint (%q)", m.chat.Tip())
	}
}

// TestUpdateNoticeSaysHowToGetIt pins what a launch-time update notice
// carries: the version, and the command that would install it here.
// Telling someone a release exists and leaving them to work out how to
// fetch it is half a message.
func TestUpdateNoticeSaysHowToGetIt(t *testing.T) {
	m := newTestModel()
	m.Update(updateMsg{latest: "9.9.9"})
	notice := ansi.Strip(m.View().Content)
	if !strings.Contains(notice, "update: v9.9.9") {
		t.Fatalf("no update news in the header:\n%s", notice)
	}
	// Whatever this machine has: a package-manager command, a go install,
	// or — when neither is recognised — the releases page.
	want := update.UpgradeCommand()
	if want == "" {
		want = render.RepoURL + "/releases"
	}
	if !strings.Contains(notice, want) {
		t.Errorf("notice does not say how to update (want %q):\n%s", want, notice)
	}
}

// TestShellNoticeGoesToTheTabInFront pins where an app-level message
// lands: in whichever tab the reader is on, in that tab's own notice
// spot. They all went to the Chat tab before, so a profile switch that
// failed while you were on the Inspector said nothing at all.
func TestShellNoticeGoesToTheTabInFront(t *testing.T) {
	for _, tab := range []int{0, 1, 2} {
		m := newTestModel()
		m.setTab(tab) // sizes the tab, as a real switch does
		if cmd := m.notifyError("profile alpha: no such credentials file"); cmd == nil {
			t.Fatal("the shell notice produced no command")
		}
		frame := ansi.Strip(m.View().Content)
		if !strings.Contains(frame, "no such credentials file") {
			t.Errorf("tab %d does not show the shell's notice:\n%s", tab, frame)
		}
		// Over the tab's own content, never on the help line — the one
		// row always saying what you can press.
		rows := strings.Split(frame, "\n")
		if !strings.Contains(rows[len(rows)-1], "cycle tabs") {
			t.Errorf("tab %d: the notice took the help line's row: %q", tab, rows[len(rows)-1])
		}
	}
}

// TestStreamingHelpSaysWhatStillWorks pins the line shown while a reply
// streams. Typing is not refused — enter queues — the command menu is
// open for everything except /profiles and /clear, and find, scroll and
// the record walk all keep working. A line that dropped the tab's
// controls read as "the app is busy, hands off", which is not what is
// happening; and a binding declared without keys reports itself disabled,
// so the help widget silently dropped it, which is how that line came to
// say nothing but "cycle tabs".
func TestStreamingHelpSaysWhatStillWorks(t *testing.T) {
	m := newTestModel()

	// Every entry the streaming tail carries must be bound: a binding
	// declared with help text and no keys reports itself disabled, and
	// the widget drops it without a word.
	for _, b := range m.keys.shortStreaming() {
		if !b.Enabled() {
			t.Errorf("streaming tail entry %q has no keys, so the widget will drop it", b.Help().Desc)
		}
	}

	var line string
	for _, b := range append(m.chat.StreamingHelp(), m.keys.shortStreaming()...) {
		if !b.Enabled() {
			t.Errorf("help entry %q has no keys, so the widget will drop it", b.Help().Desc)
		}
		line += b.Help().Key + " " + b.Help().Desc + " · "
	}
	for _, want := range []string{"enter queue message", "/ commands", "ctrl+f find", "esc interrupt", "cycle tabs"} {
		if !strings.Contains(line, want) {
			t.Errorf("the streaming help line is missing %q: %s", want, line)
		}
	}
	if strings.Contains(line, "enter send") {
		t.Errorf("the line still promises send while a reply streams: %s", line)
	}
}
