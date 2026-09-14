// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package e2e holds the TUI's end-to-end tests — black-box over the
// composed shell's public surface (tui.New, Update, View), in three
// styles:
//
//   - pumped flows: the app driven by a minimal message pump (below) —
//     commands run async and feed messages back, while the test owns the
//     model and asserts on the current frame (ANSI-stripped).
//   - teatest flows: a real headless tea.Program for lifecycle behaviors
//     (quitting) where frame-by-frame access doesn't matter.
//   - golden renders: frames pinned under testdata/; regenerate with
//     `go test ./internal/tui/e2e -update` and review the diff.
package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/tui"
)

// cfgFor is a chat-api endpoint config pointed at base; an unroutable
// base (http://127.0.0.1:9) serves tests that never send.
func cfgFor(base string) config.Config {
	return config.Config{
		Kind:    config.KindModel,
		API:     config.APIChat,
		Stream:  true,
		APIBase: base,
		APIKey:  "test-key",
		Model:   "test-model",
	}
}

// app is the pumped harness: it owns the model (all Update/View calls
// happen on the test goroutine, as Bubble Tea's loop would) and executes
// returned commands on goroutines that feed their messages back — the
// same unidirectional flow, with the frame inspectable between messages.
type app struct {
	t    *testing.T
	m    tea.Model
	msgs chan tea.Msg
	quit atomic.Bool
	// bell flags that a turn-end terminal notification (OSC 9) was
	// emitted — raw writes have no model effect to assert on otherwise.
	bell atomic.Bool
}

// newModel builds the shell as cli.runTUI does; setup hooks run between
// construction and Init/Run, exactly where runTUI calls EnableProfiles.
func newModel(t *testing.T, cfg config.Config, setup ...func(*tui.Model)) *tui.Model {
	t.Helper()
	// Init fetches the published pricing table when no local file is
	// set; default to an unroutable address so tests never touch the
	// network (a test exercising the fetch sets its own mock URL first).
	if os.Getenv("PRICING_URL") == "" {
		t.Setenv("PRICING_URL", "http://127.0.0.1:9/pricing.yaml")
	}
	// settings.json goes beside the config file; a test that has not
	// pinned one must not find the developer's.
	if os.Getenv("CONFIG_FILE") == "" {
		t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "aibench.yaml"))
	}
	captureCh := make(chan capture.Event, 64)
	client, err := provider.NewClient(cfg, captureCh)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	m := tui.New(cfg, client, captureCh, make(chan struct{}, 1))
	for _, fn := range setup {
		fn(m)
	}
	return m
}

// newClientlessApp mirrors the launch path when profiles exist: the shell
// is built with no config and no client, and Init selects the active
// profile through the ordinary switch — so credentials are read there,
// not before.
func newClientlessApp(t *testing.T, w, h int, setup ...func(*tui.Model)) *app {
	t.Helper()
	if os.Getenv("PRICING_URL") == "" {
		t.Setenv("PRICING_URL", "http://127.0.0.1:9/pricing.yaml")
	}
	m := tui.New(config.Config{}, nil, make(chan capture.Event, 64), make(chan struct{}, 1))
	for _, fn := range setup {
		fn(m)
	}
	a := &app{t: t, m: m, msgs: make(chan tea.Msg, 64)}
	a.exec(m.Init())
	a.send(tea.WindowSizeMsg{Width: w, Height: h})
	return a
}

// newApp wraps a fresh model in the pump and sizes it.
func newApp(t *testing.T, cfg config.Config, w, h int, setup ...func(*tui.Model)) *app {
	t.Helper()
	m := newModel(t, cfg, setup...)
	a := &app{t: t, m: m, msgs: make(chan tea.Msg, 64)}
	a.exec(m.Init())
	a.send(tea.WindowSizeMsg{Width: w, Height: h})
	return a
}

// send applies one message and runs whatever command it returns.
func (a *app) send(msg tea.Msg) {
	m, cmd := a.m.Update(msg)
	a.m = m
	a.exec(cmd)
}

// exec runs a command on a goroutine, feeding its message back into the
// pump; batches fan out. A QuitMsg only flags — the pump has no program
// to stop.
func (a *app) exec(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		switch msg := msg.(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range msg {
				a.exec(c)
			}
		case tea.QuitMsg:
			a.quit.Store(true)
		case tea.RawMsg:
			// Raw terminal writes (the turn-end bell/OSC 9) have no model
			// effect to assert on — flag them for the tests instead.
			if strings.Contains(fmt.Sprint(msg.Msg), "\x1b]9;") {
				a.bell.Store(true)
			}
		default:
			a.msgs <- msg
		}
	}()
}

// typeText feeds s rune by rune, as key presses.
func (a *app) typeText(s string) {
	for _, r := range s {
		a.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

// view is the current frame with its styling; plain strips it — text
// assertions must not depend on where style runs split a phrase.
func (a *app) view() string  { return a.m.(*tui.Model).View().Content }
func (a *app) plain() string { return ansiRE.ReplaceAllString(a.view(), "") }

// frameDeadline bounds a wait. It is generous on purpose: it costs
// nothing while a test passes (the wait ends on the frame, not the
// clock), and it is only ever reached by a test that is going to fail
// anyway. A tighter bound turned a loaded CI runner into red builds —
// two different waits timed out at exactly 8s on runs whose assertions
// were fine, which is a harness saying "slow", not a product saying
// "wrong".
const frameDeadline = 45 * time.Second

// waitFrame pumps queued messages until the current frame satisfies ok.
func (a *app) waitFrame(ok func(plain string) bool) {
	a.t.Helper()
	deadline := time.After(frameDeadline)
	for {
		if ok(a.plain()) {
			return
		}
		select {
		case msg := <-a.msgs:
			a.send(msg)
		case <-deadline:
			a.t.Fatalf("frame condition not met after %s; last frame:\n%s", frameDeadline, a.plain())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// waitText pumps until every want is on the current frame together.
func (a *app) waitText(wants ...string) {
	a.t.Helper()
	a.waitFrame(func(p string) bool {
		for _, w := range wants {
			if !strings.Contains(p, w) {
				return false
			}
		}
		return true
	})
}

// row returns the index of the first frame line containing s.
func (a *app) row(s string) int {
	a.t.Helper()
	for i, line := range strings.Split(a.plain(), "\n") {
		if strings.Contains(line, s) {
			return i
		}
	}
	a.t.Fatalf("no frame line contains %q", s)
	return -1
}
