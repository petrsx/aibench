// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

import (
	"log/slog"
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/prefs"
	"github.com/petrsx/aibench/internal/provider"
)

// Profiles: aibench.yaml selects endpoints; /profiles switches between them and
// the config file hot-reloads like the prompt file. Empty when no config
// file is active.

// EnableProfiles arms in-app profile switching from the config file.
// active is the session's starting profile — the cli resolves it from the
// app's settings file (prefs.ActiveProfile).
func (m *Model) EnableProfiles(f config.File, active string, factory func(config.Config) (provider.Client, error), configCh <-chan struct{}) {
	if len(f.Profiles) == 0 {
		return
	}
	m.cfgFile = f
	m.clientFactory = factory
	m.configCh = configCh
	m.activeProfile = active
	m.profileNames = slices.Sorted(maps.Keys(f.Profiles))
	m.registerCommands() // /profiles and /profiles-edit exist only with a config file
	m.chat.SetProfile(m.spec, m.cfg, m.profileLine())
	m.prompt.SetProfile(m.spec, m.cfg.PromptFile, m.cfg.ToolsFiles, m.profileLine())
	// The startup selection is a profile switch like any other, performed
	// on Init: one code path resolves credentials, builds the client, and
	// reports failure as a notice — so a broken profile can never keep the
	// app off the screen.
	m.pendingProfile = active
}

// selectPending performs the launch-time profile switch (see
// EnableProfiles). Returns nil when there is nothing pending (a model
// built without profiles, as the tests do).
func (m *Model) selectPending() tea.Cmd {
	name := m.pendingProfile
	m.pendingProfile = ""
	if name == "" {
		return nil
	}
	return m.switchProfile(name)
}

// switchProfile makes name the live endpoint: new config and client, the
// provider's spec and param inputs, its prompt file. Moving to a
// *different* profile clears the conversation exactly like /clear — a new
// endpoint means a new prompt and params, and dragging context the new
// prompt never saw would test neither profile. A re-resolve of the profile
// already active (the config file hot-reload) keeps the conversation: the
// endpoint did not change, its knobs did. Failures keep the old client and
// notify.
func (m *Model) switchProfile(name string) tea.Cmd {
	if m.state.Streaming() || m.clientFactory == nil || name == "" {
		return nil
	}
	cfg, err := m.cfgFile.Resolve(name)
	if err != nil {
		return m.notifyError(err.Error())
	}
	client, err := m.clientFactory(cfg)
	if err != nil {
		return m.notifyError(err.Error())
	}
	// The prompt set comes with the profile — its own pin, else the
	// config's first set. Nothing is carried over from the profile being
	// left: an endpoint is tested with the prompt it declares, and a set
	// that followed the reader between profiles would make a run depend
	// on where they had been. A pin naming a missing set fails the
	// switch loudly — an explicit reference is a config error.
	promptSet := config.ActivePromptSet(m.cfgFile, name)
	if err := m.cfgFile.ApplyPrompt(&cfg, promptSet); err != nil {
		return m.notifyError(err.Error())
	}
	m.stopScript()    // a script must not continue across endpoints
	m.sendQueue = nil // nor may messages queued for the previous one
	if name != m.activeProfile {
		m.clearSession()
	}
	m.cfg, m.spec = cfg, provider.SpecFor(cfg.API)
	m.state.SetClient(client)
	m.activeProfile = name
	m.activePrompt = promptSet
	slog.Info("profile switch", "profile", name, "kind", cfg.Kind, "api", cfg.API, "model", cfg.Model, "prompt", promptSet)
	m.prompt.SetProfile(m.spec, cfg.PromptFile, cfg.ToolsFiles, m.profileLine())
	m.chat.SetProfile(m.spec, m.cfg, m.profileLine())
	m.setStarters(cfg.StartersFiles)
	// Best-effort persistence into the app's own settings file — the
	// human's config is never written.
	_ = prefs.SaveActiveProfile(prefs.Path(config.FilePath()), name)
	return m.reportMissingFiles()
}

// reportMissingFiles raises what the prompt set named and the disk does
// not have. It is a notice rather than a failed switch: the endpoint is
// still reachable and a request will still go out — which is exactly why
// it has to be said, because the header will show the set's name while
// the model answers as if nothing had been sent with it.
func (m *Model) reportMissingFiles() tea.Cmd {
	issues := m.prompt.LoadIssues()
	for _, f := range m.missingStarters {
		issues = append(issues, "starter script not found: "+f)
	}
	if len(issues) == 0 {
		return nil
	}
	return m.notifyError(strings.Join(issues, " · "))
}

// reloadConfig re-reads aibench.yaml and re-resolves the active profile in
// place — the one path both a watcher signal and /profiles-edit's editor take back
// into the app. A file that no longer parses keeps the running profile and
// says so: a silent drop would leave the app on an endpoint the file no
// longer describes.
func (m *Model) reloadConfig() tea.Cmd {
	f, ok, err := config.LoadFile(config.FilePath())
	if err != nil {
		slog.Warn("config reload failed", "err", err)
		return m.notifyError(err.Error())
	}
	if !ok {
		return nil
	}
	m.cfgFile = f
	m.profileNames = slices.Sorted(maps.Keys(f.Profiles))
	name := m.activeProfile
	if _, exists := f.Profiles[name]; !exists {
		name = prefs.ActiveProfile(f, prefs.Path(config.FilePath()))
	}
	return m.switchProfile(name)
}
