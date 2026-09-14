// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

// Slash commands: the app's actions, typed in the composer — /settings,
// /profiles, /profiles-edit, … The menu lives in the composer (internal/tui/chat:
// "/" lists, ↑/↓ cycle, tab completes, enter runs), the meaning lives
// here in one registry. Commands that pick something (a profile, a
// session, a starter prompt) carry their options into the menu's second
// stage — the Claude Code /model shape: the list appears inline under
// the composer, never as a modal.

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/tui/chat"
)

// Command names — the shell's own vocabulary, matched in runCommand.
const (
	cmdConfig      = "settings"
	cmdProfile     = "profiles"
	cmdEdit        = "profiles-edit"
	cmdStarters    = "starters"
	cmdClear       = "clear"
	cmdInit        = "init"
	cmdPricing     = "pricing-update"
	cmdPricingEdit = "pricing-edit"
	cmdHelp        = "help"
	cmdQuit        = "exit"
)

// registerCommands hands the composer the commands that make sense right
// now: without a config file there are no profiles to switch, no
// sessions to load, and nothing to edit. Called wherever that changes.
// The shell keeps the list too — the key sheet renders it.
func (m *Model) registerCommands() {
	// Two groups, conditions only leaving things out. The session
	// commands — what is being tested: the profile, the script, the
	// conversation — come first in the order a session uses them; the
	// housekeeping below them sorts a→z, since the menu is filtered by
	// typing and nobody learns a ranking, everybody knows the alphabet.
	all := []struct {
		cmd  chat.Command
		when bool
	}{
		{chat.Command{
			Name: cmdProfile, Desc: "switch endpoint profile", Options: m.profileOptions, Session: true,
			Title:  "Select profile",
			Header: "a different prompt takes over — the conversation is cleared",
		}, len(m.profileNames) > 0},
		{chat.Command{
			Name: cmdStarters, Desc: "run a starter script", Options: m.starterOptions, Session: true,
			Title:  "Run starter script",
			Header: "send the script's prompts in order, each after the previous reply",
		}, len(m.scripts) > 0},
		{chat.Command{Name: cmdClear, Desc: "clear the conversation, start fresh", Session: true}, true},
		{chat.Command{Name: cmdEdit, Desc: "open aibench.yaml in your editor"}, true},
		{chat.Command{Name: cmdInit, Desc: "scaffold ./.aibench for this folder"}, true},
		{chat.Command{Name: cmdPricing, Desc: "refresh pricing.yaml from catwalk"}, true},
		{chat.Command{Name: cmdPricingEdit, Desc: "open pricing.yaml in your editor"}, true},
		{chat.Command{Name: cmdConfig, Desc: "settings: theme, editor, startup profile"}, true},
		{chat.Command{Name: cmdHelp, Desc: "keys and commands (the Help tab)"}, true},
		{chat.Command{Name: cmdQuit, Desc: "leave the app"}, true},
	}
	cmds := make([]chat.Command, 0, len(all))
	for _, c := range all {
		if c.when {
			cmds = append(cmds, c.cmd)
		}
	}
	slices.SortStableFunc(cmds, func(a, b chat.Command) int {
		switch {
		case a.Session != b.Session: // session group first
			if a.Session {
				return -1
			}
			return 1
		case a.Session: // declared order within the group
			return 0
		}
		return strings.Compare(a.Name, b.Name)
	})
	m.commandList = cmds
	m.chat.SetCommands(cmds)
}

// profileOptions lists the profiles for /profiles' second stage, the
// active one marked.
func (m *Model) profileOptions() []chat.Option {
	opts := make([]chat.Option, len(m.profileNames))
	for i, name := range m.profileNames {
		desc := ""
		if p, ok := m.cfgFile.Profiles[name]; ok {
			desc = p.API.Model + " · " + p.Kind
		}
		title := name
		if name == m.activeProfile {
			title += " ✔"
		}
		opts[i] = chat.Option{Name: title, Desc: desc}
	}
	return opts
}

// starterOptions lists the active set's starter scripts for /starters.
func (m *Model) starterOptions() []chat.Option {
	opts := make([]chat.Option, len(m.scripts))
	for i, sc := range m.scripts {
		desc := fmt.Sprintf("%d prompts", len(sc.Prompts))
		if len(sc.Prompts) > 0 {
			desc += " · " + preview(sc.Prompts[0].Title, 40)
		}
		opts[i] = chat.Option{Name: sc.Name, Desc: desc}
	}
	return opts
}

// runCommand performs one entered command. Unknown names never reach
// here — the composer only emits what it offered or what entered()
// matched.
func (m *Model) runCommand(msg chat.CommandMsg) tea.Cmd {
	// The commands that replace endpoint or store state wait for the
	// stream, exactly as their pickers used to refuse to open.
	switch msg.Name {
	case cmdProfile, cmdClear:
		if m.state.Streaming() {
			return m.notifyWarn("not while a reply streams — esc interrupts it first")
		}
	}
	switch msg.Name {
	case cmdConfig:
		m.openSettings("")
	case cmdProfile:
		name := strings.TrimSuffix(msg.Args, " ✔") // the active-profile mark, if picked
		if name == "" {
			return nil // enter on the bare command opens the options instead
		}
		if _, ok := m.cfgFile.Profiles[name]; !ok {
			return m.notifyError("no profile named " + name)
		}
		return m.switchProfile(name)
	case cmdEdit:
		return m.editConfig()
	case cmdStarters:
		return m.runStarters(msg.Args)
	case cmdClear:
		return m.newSession()
	case cmdInit:
		return m.initLocalConfig()
	case cmdPricing:
		return m.updatePricing()
	case cmdPricingEdit:
		return m.editPricing()
	case cmdHelp:
		m.openHelp()
	case cmdQuit:
		return tea.Quit
	}
	return nil
}

// runStarters performs a /starters pick: play the named script (bare
// /starters runs the first — the launch offer's enter-to-run default).
func (m *Model) runStarters(pick string) tea.Cmd {
	if pick == "" {
		return m.startScript(0)
	}
	for i, sc := range m.scripts {
		if sc.Name == pick {
			return m.startScript(i)
		}
	}
	return m.notifyError("no starter script named " + pick)
}

// preview is the first line of text, capped to width runes.
func preview(text string, width int) string {
	for i, r := range text {
		if r == '\n' {
			text = text[:i]
			break
		}
	}
	runes := []rune(text)
	if len(runes) > width {
		return string(runes[:width-1]) + "…"
	}
	return text
}

// initLocalConfig (/init) scaffolds ./.aibench in the current folder:
// the starter aibench.yaml, exactly what a fresh install writes at the
// user level (~/.aibench). The config search
// prefers the local folder (./aibench.yaml → ./.aibench/aibench.yaml →
// ~/.aibench/aibench.yaml, first hit wins), so a folder with its own
// .aibench gets its own profiles, prompts, and sessions — and one
// without falls back to the user-level config, unchanged. The running
// session keeps the config it launched with; the notice says so.
func (m *Model) initLocalConfig() tea.Cmd {
	if _, err := os.Stat("aibench.yaml"); err == nil {
		return m.notifyWarn("this folder already has aibench.yaml")
	}
	path := filepath.Join(".aibench", "aibench.yaml")
	if _, err := os.Stat(path); err == nil {
		return m.notifyWarn("already initialized — " + path + " exists")
	}
	if err := config.Scaffold(path); err != nil {
		return m.notifyError("init failed: " + err.Error())
	}
	slog.Info("init", "scaffolded", path)
	return m.notifySay(
		"created " + path + " — set api.base/model + credentials, then restart aibench here")
}
