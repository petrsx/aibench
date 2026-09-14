// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package app is the composition layer: it wires config, provider
// client, watchers, and the TUI together and runs the program — the
// launch path the bare `aibench` command delegates to. Commands live in
// internal/cli; the packages below stay independent of each other.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/applog"
	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/fswatch"
	"github.com/petrsx/aibench/internal/prefs"
	"github.com/petrsx/aibench/internal/pricing"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/tui"
	"github.com/petrsx/aibench/internal/version"
)

// repoURL is where a crash gets reported.
const repoURL = "https://github.com/petrsx/aibench"

// Run resolves the environment into a running TUI: config + active
// profile, the capturing provider client, the hot-reload watchers, the
// composed shell.
func Run() error {
	// aibench.yaml is required: profiles select the endpoint, and the
	// environment supplies credentials only (each profile names its own
	// file, read at selection time — a stray ./.env never authenticates
	// anything on its own). No file anywhere is a fresh install, which
	// scaffolds one; a pinned path that is missing is an error to report.
	cfgFile, hasProfiles, err := config.LoadFile(config.FilePath())
	if err != nil {
		return fmt.Errorf("config file error: %w", err)
	}
	if !hasProfiles {
		if config.Pinned() {
			return fmt.Errorf("config file not found: %s", config.FilePath())
		}
		return firstRun()
	}

	// The active selection lives in the app's settings file beside the
	// config: a pinned startup profile, else the last-used one, else the
	// first profile in name order.
	active := prefs.ActiveProfile(cfgFile, prefs.Path(config.FilePath()))

	// The launch reads no credentials at all: the shell selects `active`
	// on Init through the ordinary switch path, which resolves, builds the
	// client, and reports failure as a notice. One unusable profile
	// therefore never keeps the app off the screen.
	slog.Info("tui", "profile", active)

	captureCh := make(chan capture.Event, 64)
	factory := func(c config.Config) (provider.Client, error) { return provider.NewClient(c, captureCh) }

	// The prompt and starter files, the config, and pricing.yaml all
	// hot-reload the same way: a watcher signals its channel, the TUI
	// re-reads the file on the message that follows. A watch that cannot
	// be set up is not fatal — that file simply won't reload.
	presetCh := make(chan struct{}, 1)
	configCh := make(chan struct{}, 1)
	pricingCh := make(chan struct{}, 1)
	watches := []struct {
		what string
		path string
		ch   chan struct{}
	}{
		{"config", config.FilePath(), configCh},
		{"pricing", pricing.FilePath(), pricingCh},
	}
	// Every prompt set's instructions + starter files get a watcher, so
	// hot-reload keeps working whichever profile is active.
	for _, f := range cfgFile.PromptFiles() {
		watches = append(watches, struct {
			what string
			path string
			ch   chan struct{}
		}{"prompt", f, presetCh})
	}
	for _, w := range watches {
		if err := fswatch.Watch(context.Background(), w.path, w.ch); err != nil {
			slog.Warn(w.what+" file watch disabled", "file", w.path, "err", err)
		}
	}

	// A nil client is a legal state: Init installs the real one through
	// the profile switch; the shell refuses sends until then.
	m := tui.New(config.Config{}, nil, captureCh, presetCh)
	m.EnableProfiles(cfgFile, active, factory, configCh)
	m.SetPricing(pricing.Runtime())
	m.WatchPricing(pricingCh)
	// Terminal features (alt-screen, mouse, focus reporting) are declared on
	// the shell's tea.View each render — no program options in v2.
	p := tea.NewProgram(m)
	_, err = p.Run()
	return runResult(err)
}

// runResult turns the program's exit into what the user should see.
// Bubble Tea catches panics itself: it restores the terminal, prints the
// panic and stack to stderr, and hands back ErrProgramPanic — so the
// crash is already survivable and the remaining job is to make it
// reportable. The stack is on a screen that scrolls away, so the crash
// also goes to the app log, where it can still be found.
//
// A killed or interrupted program is a normal exit, not a failure: ^C
// must not print an error.
func runResult(err error) error {
	switch {
	case err == nil:
		slog.Info("exit")
		return nil
	case errors.Is(err, tea.ErrInterrupted):
		slog.Info("exit", "reason", "interrupted") // ^C, the ordinary way out
		return nil
	case errors.Is(err, tea.ErrProgramKilled):
		// Not a quit: the program's input loop ended under it. Silent on
		// screen — the terminal is already restored and there is nothing
		// useful to say to someone who did not ask for this — but the log
		// must say so, or an app that vanishes on start leaves no trace of
		// having tried.
		slog.Warn("exit", "reason", "program killed",
			"note", "the input loop ended; the app did not quit on its own")
		return nil
	case errors.Is(err, tea.ErrProgramPanic):
		slog.Error("tui crashed", "err", err, "version", version.Version)
		return fmt.Errorf("aibench crashed — your terminal has been restored\n\n"+
			"the panic and its stack trace are printed above\n"+
			"what led up to it is in %s\n"+
			"please report it, with both, at %s/issues", applog.Path(), repoURL)
	default:
		slog.Error("tui exited with an error", "err", err)
		return err
	}
}

// firstRun scaffolds the starter config at the user-level default
// location (the last stop of the config search, so the next launch finds
// it), drops the schema copy beside it for editor completion, and prints
// what to do next instead of starting the TUI.
func firstRun() error {
	path, err := config.DefaultPath()
	if err != nil {
		return fmt.Errorf("no configuration found and no home directory to create one in: %w", err)
	}
	if err := config.Scaffold(path); err != nil {
		return fmt.Errorf("no configuration found; creating a starter one failed: %w", err)
	}
	slog.Info("first run", "scaffolded", path)
	fmt.Printf("no configuration found — created a starter one:\n"+
		"\n"+
		"  %s\n"+
		"\n"+
		"  1. set api.base and api.model to your endpoint\n"+
		"  2. put the secrets in a .env file beside it (see examples/settings/.env.example\n"+
		"     in the repo for the auth method → variables contract)\n"+
		"  3. run aibench again\n"+
		"\n"+
		"One short profile per scenario lives at examples/settings/aibench.yaml,\n"+
		"ready-made prompt presets in examples/prompts/, and every knob in\n"+
		"docs/configuration.md. \"aibench schema\" prints the config's JSON Schema;\n"+
		"a copy sits beside the file for editor completion.\n"+
		"\"aibench --help\" lists the headless commands.\n", path)
	return nil
}
