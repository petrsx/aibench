// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package tui is the Bubble Tea shell: it composes the three tabs (Chat /
// Inspector / Prompt), holds the shared store + state, and routes messages.
// Each tab is an independent tea.Model in its own package; the shell holds no
// view logic beyond the header, tab bar, and help line. The request lifecycle
// (Send / Receive / Capture) lives in internal/state; the shell only routes
// messages into it and bridges the Prompt tab's system prompt/params into Send.
package tui

import (
	"context"
	"log/slog"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/prefs"
	"github.com/petrsx/aibench/internal/pricing"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/state"
	"github.com/petrsx/aibench/internal/store"
	"github.com/petrsx/aibench/internal/tui/chat"
	"github.com/petrsx/aibench/internal/tui/inspector"
	"github.com/petrsx/aibench/internal/tui/notify"
	"github.com/petrsx/aibench/internal/tui/prompt"
	"github.com/petrsx/aibench/internal/tui/render"
	"github.com/petrsx/aibench/internal/tui/screen"
	"github.com/petrsx/aibench/internal/tui/theme"
	"github.com/petrsx/aibench/internal/update"
	"github.com/petrsx/aibench/internal/version"
)

const (
	headerRows = 3 // logo left + right-docked tab boxes sharing the rows
	tabBarRow  = 1 // screen row of the tab labels; clicks land on rows 0-2
)

type (
	configMsg struct{}                // config file changed on disk
	updateMsg struct{ latest string } // a newer release exists
	// pricingMsg carries the published rates, fetched only when the
	// session found no local pricing.yaml to read.
	pricingMsg pricing.Table
	// escTimeoutMsg repaints once the dev quit window lapses, so the hint
	// it put in the help line goes away on its own.
	escTimeoutMsg struct{}
)

// Model is the shell.
type Model struct {
	cfg     config.Config
	spec    provider.Spec // how this api presents records in the UI
	pricing pricing.Table // model id → rates, forwarded to the Chat tab

	// state is the live layer over the session store (Send / Receive / Capture
	// + progress); it embeds the store. The tabs hold it and pull their views
	// from it per render — the shell pushes nothing.
	state *state.State

	// the three tabs, each an independent tea.Model. Concrete fields for typed
	// calls (SetProfile, Params, …); tabs is the registry the shell iterates
	// for switching, focus, and rendering.
	chat      *chat.Model
	inspector *inspector.Model
	prompt    *prompt.Model
	tabs      [tabCount]tab

	activeTab int

	// updateNews is the "a newer build exists, here is how to get it"
	// line, set once by the launch check. It takes the header's hint slot
	// for the rest of the session (header.go).
	updateNews string
	tabSpans   [tabCount][2]int // column span of each tab label, for clicks
	hlp        help.Model       // short main controls; /help opens the full keymap
	keys       globalKeys       // the shell-owned chords; tabs own the rest

	// profiles: aibench.yaml selects endpoints; /profiles switches, the config
	// file hot-reloads like the prompt file. Empty when no config file.
	cfgFile      config.File
	profileNames []string
	// missingStarters are declared scripts that would not load; the
	// profile switch reports them with the rest (profiles.go).
	missingStarters []string
	activeProfile   string
	clientFactory   func(config.Config) (provider.Client, error)
	configCh        <-chan struct{}
	pricingCh       <-chan struct{} // pricing.yaml watcher (pricing.go)

	width  int
	height int
	ready  bool
	// focused mirrors the terminal's focus reporting; a reply that lands
	// while unfocused rings the turn-end notification.
	focused bool

	// dev turns on the developer affordances (dev.go). lastEsc is the one
	// that needs state: it arms the quit window a second esc closes, and
	// stays zero in a normal build.
	dev     bool
	lastEsc time.Time

	// commandList mirrors what registerCommands handed the composer, for
	// the key sheet's commands group.
	commandList []chat.Command
	// settings is the open settings screen (/settings), nil when closed. It
	// takes over the content region rather than floating over it — see
	// internal/tui/screen. prefs are the values it edits, loaded once
	// at construction and written back on every change.
	settings *screen.Model
	prefs    prefs.Settings
	// termDark is what the terminal reported about its background; the
	// theme preference overrides it, "auto" follows it.
	termDark bool
	// the prompt axis: the set the active profile resolved to and its
	// loaded starter scripts (prompts.go); the composer's history seed
	// carries the flattened texts.
	activePrompt string
	scripts      []starterScript

	// script playback: one starter script's prompts sent in order (the
	// TUI twin of `send --script`); scriptSet/scriptIdx name the run.
	scriptRunning bool
	scriptSet     int
	scriptIdx     int
	// kittyKeys: the terminal granted keyboard enhancements (key
	// disambiguation), so chords like shift+enter arrive as distinct keys.
	kittyKeys bool

	// pendingProfile is the launch-time selection, applied on Init
	// through the ordinary switch path (see EnableProfiles).
	pendingProfile string

	// sendQueue holds messages typed while a send was in flight, in the
	// order they were entered; drained one per completed send.
	sendQueue []string
}

func New(cfg config.Config, client provider.Client, captureCh chan capture.Event, presetCh <-chan struct{}) *Model {
	dev := devMode()
	// The spinner stands in for the assistant dot while a response streams
	// (Claude Code-style star cycle); the Chat tab paints the frame the shell
	// reports, and swaps in the static dot once the turn completes.
	sp := spinner.New()
	sp.Spinner = spinner.Spinner{
		Frames: []string{"✢", "✳", "✶", "✻", "✽", "✻", "✶", "✳"},
		FPS:    time.Second / 8,
	}
	sp.Style = theme.SpinnerStyle

	// The app's own preferences (theme, editor, startup profile, the two
	// background switches) live in settings.json beside the config file;
	// a missing file means the defaults.
	settings := prefs.Load(prefs.Path(config.FilePath()))

	hl := help.New()
	hl.Styles.ShortKey = theme.HelpKeyStyle
	hl.Styles.ShortDesc = theme.HelpDescriptionStyle
	hl.Styles.ShortSeparator = theme.HelpDescriptionStyle
	hl.Styles.FullKey = theme.HelpKeyStyle
	hl.Styles.FullDesc = theme.HelpDescriptionStyle
	hl.Styles.FullSeparator = theme.HelpDescriptionStyle

	m := &Model{
		cfg:     cfg,
		spec:    provider.SpecFor(cfg.API),
		hlp:     hl,
		keys:    newGlobalKeys(dev),
		prefs:   settings,
		dev:     dev,
		focused: true, // terminals report the initial state only on change
	}
	m.state = state.New(store.New(500), client, captureCh, sp)
	m.prompt = prompt.New(m.spec, cfg.PromptFile, cfg.ToolsFiles, m.profileLine(), presetCh)
	m.inspector = inspector.New(m.state)
	m.chat = chat.New(m.state, m.spec, m.cfg, m.pricing, m.profileLine())
	m.setStarters(cfg.StartersFiles)
	m.tabs = [tabCount]tab{
		tabChat:      {name: "Chat", model: m.chat},
		tabInspector: {name: "Inspector", model: m.inspector},
		tabPrompt:    {name: "Prompt", model: m.prompt},
	}
	m.registerCommands()
	// A declared theme applies before the first frame; "auto" waits for
	// the terminal's background reply (dark until then).
	if settings.Theme != prefs.ThemeAuto {
		m.applyThemePref()
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	// RequestBackgroundColor: the terminal's reply drives the light/dark
	// palette (dark is the default until it lands).
	cmds := []tea.Cmd{
		m.chat.Init(), m.state.WaitCapture(), m.prompt.Init(), m.waitConfig(),
		m.waitPricing(), m.selectPending(), tea.RequestBackgroundColor, tipTick(),
	}
	if m.prefs.UpdateCheck {
		cmds = append(cmds, checkUpdate)
	}
	// Only with no local pricing.yaml is the published table fetched, so a
	// fresh install shows costs without being told to run a command — and
	// a session that already has rates calls no one. `aibench pricing
	// update` is how that file comes to exist, from catwalk.
	if len(m.pricing) == 0 {
		cmds = append(cmds, fetchPublishedPricing)
	}
	return tea.Batch(cmds...)
}

// checkUpdate asks GitHub (cached, 3s budget, dev builds no-op) whether
// a newer release exists; the shell shows a one-time corner notice.
func checkUpdate() tea.Msg {
	if latest, newer := update.Check(context.Background(), version.Version); newer {
		return updateMsg{latest: latest}
	}
	return nil
}

// dequeue takes the oldest queued message, if any.
func (m *Model) dequeue() (string, bool) {
	if len(m.sendQueue) == 0 {
		return "", false
	}
	next := m.sendQueue[0]
	m.sendQueue = m.sendQueue[1:]
	return next, true
}

// send bridges the Prompt tab into state.Send: the shell reads the system
// prompt + params + tools (state must not import a view) and
// drives the request. The tabs re-derive from state on their next render;
// the shell pushes nothing.
func (m *Model) send(text string) tea.Cmd {
	if !m.state.Ready() {
		hint := "no endpoint: this profile has no working credentials"
		if len(m.profileNames) > 1 {
			hint += " — pick another with the profile picker"
		}
		return m.notifyError(hint)
	}
	return m.state.Send(m.prompt.SystemPrompt(), m.prompt.Params(), m.prompt.Request(), text)
}

// waitConfig re-arms the config-file watcher signal; nil channel blocks
// forever, which is fine for a tea command.
func (m *Model) waitConfig() tea.Cmd {
	return func() tea.Msg { <-m.configCh; return configMsg{} }
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		firstReady := !m.ready
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true
		// Starter scripts on launch: declaring them opens the /starters
		// selector — an offer, never a send. Enter runs the highlighted
		// script, esc clears the composer and nothing happens (the
		// successor of the deleted auto-play).
		if firstReady && len(m.scripts) > 0 {
			m.chat.Insert("/starters ")
		}
		return m, nil

	case tea.MouseMsg:
		return m, m.handleMouse(msg)

	// Terminal focus reporting (ReportFocus on the view): park the cursor while
	// the window is unfocused, restore it on return.
	case tea.BlurMsg:
		m.focused = false
		for i := range m.tabs {
			m.tabs[i].model.Blur()
		}
		return m, nil
	case tea.FocusMsg:
		m.focused = true
		return m, m.tabs[m.activeTab].model.Focus()

	case tea.KeyboardEnhancementsMsg:
		m.kittyKeys = msg.SupportsKeyDisambiguation()
		// The Chat tab shows shift+enter vs alt+enter for newline based on this.
		m.chat.SetKittyKeys(m.kittyKeys)
		return m, nil

	case tea.BackgroundColorMsg:
		// What the terminal reports is the "auto" answer; an explicit
		// theme preference outranks it (applyThemePref decides).
		m.termDark = msg.IsDark()
		m.applyThemePref() // re-derives the active tab's cached renders too
		return m, nil

	case tea.KeyPressMsg:
		if cmd, done := m.handleKey(msg); done {
			return m, cmd
		}

	case state.CaptureMsg:
		m.state.Capture(capture.Event(msg))
		return m, m.state.WaitCapture()

	// The Prompt tab owns its file: reload and debounced-save messages are its
	// own, routed here even while another tab is active. The starter
	// scripts ride the same watch signal — re-seed the composer.
	case prompt.ReloadMsg, prompt.SaveMsg:
		if _, ok := msg.(prompt.ReloadMsg); ok {
			m.setStarters(m.cfg.StartersFiles)
		}
		return m, m.prompt.Update(msg)

	case configMsg:
		// The config file changed on disk: reload it and re-resolve the active
		// profile in place, so editing aibench.yaml behaves like editing the
		// prompt file.
		return m, tea.Batch(m.reloadConfig(), m.waitConfig())

	// A terminal editor exited: re-read the edited file right away rather
	// than wait on the watcher, so the edit is live the moment the screen is.
	case editorDoneMsg:
		if msg.err != nil {
			slog.Warn("editor failed", "err", msg.err)
			return m, m.notifyError("editor: " + msg.err.Error())
		}
		if msg.path == config.FilePath() {
			return m, m.reloadConfig()
		}
		m.reloadPricing()
		return m, nil

	// The Chat tab validated an Enter; the shell assembles and drives the send.
	case chat.SendMsg:
		// A send already in flight: hold this one and dispatch it when the
		// current one finishes (tool rounds included). Typing while waiting
		// is the normal rhythm of a testing session — dropping the
		// keystroke, as this used to, loses work silently.
		if m.state.Streaming() {
			m.sendQueue = append(m.sendQueue, msg.Text)
			m.chat.SetQueued(m.sendQueue)
			return m, nil
		}
		return m, m.send(msg.Text)

	// A slash command entered in the composer (the typed twin of the
	// global chords); the registry and the actions live in commands.go.
	case chat.CommandMsg:
		return m, m.runCommand(msg)

	// Typing "exit" quits — via the shell, like every quit path.
	case chat.QuitRequestMsg:
		return m, tea.Quit

	case pricingMsg:
		m.SetPricing(pricing.Table(msg))
		return m, nil

	case pricingReloadMsg:
		m.reloadPricing()
		return m, m.waitPricing()

	case pricingUpdatedMsg:
		return m, m.pricingUpdated(msg)

	case updateMsg:
		slog.Info("update available", "latest", msg.latest, "current", version.Version)
		// Say how to get it, not only that it exists: the command is
		// chosen from what is installed here (update.UpgradeCommand), and
		// where none of the channels is recognised the releases page is
		// the honest answer — linked, so it is one click. It rides the
		// header in place of the rotating tip, for the session's whole
		// length: a corner notice that lapses after a few seconds is the
		// wrong shape for something a reader may want to act on later.
		how := update.UpgradeCommand()
		if how == "" {
			how = render.Link(render.RepoURL+"/releases", render.RepoURL+"/releases")
		}
		m.updateNews = "v" + msg.latest + " · " + how
		return m, nil

	case tipTickMsg:
		// Only the tab in front turns over — see tips.go.
		m.tabs[m.activeTab].model.NextTip()
		return m, tipTick()

	case state.ResponseMsg:
		cmd := m.state.Receive(provider.Delta(msg))
		if cmd == nil {
			// The send fully completed (tool rounds included): anything the
			// user typed while waiting goes first, then a running script
			// sends its next prompt, and an unfocused window gets the
			// out-of-band announcement.
			if next, ok := m.dequeue(); ok {
				m.chat.SetQueued(m.sendQueue)
				cmd = m.send(next)
			} else {
				cmd = m.scriptNext()
			}
			if !m.focused {
				cmd = tea.Batch(cmd, notify.Terminal("aibench: reply ready · "+m.cfg.Model))
			}
		}
		return m, cmd

	// A tool run finished; state feeds it back and, once the round is
	// complete, dispatches the follow-up request.
	case state.ToolResultMsg:
		return m, m.state.ToolResult(msg)

	case escTimeoutMsg:
		return m, nil

	case spinner.TickMsg:
		return m, m.state.Tick(msg)
	}

	// Everything else — tab-local keys, mouse, and widget messages like cursor
	// blinks — belongs to the active tab.
	return m, m.updateTab(msg)
}
