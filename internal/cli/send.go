// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/petrsx/aibench/internal/bindings"
	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/prefs"
	"github.com/petrsx/aibench/internal/preset"
	"github.com/petrsx/aibench/internal/provider"
	"github.com/petrsx/aibench/internal/state"
	"github.com/petrsx/aibench/internal/store"
)

// sendCmd is the one-shot, non-TUI path: send a message through a profile
// and stream the reply to stdout — a smoke test for profiles more than a
// chat client. It is the TUI's send with no screen: the same store and
// state (assembly, the tool loop, records), driven by a message pump in
// place of Bubble Tea (conversation.pump), so what a keystroke in the app
// sends and what this sends can only be the same thing.
func sendCmd() *cobra.Command {
	var profile string
	var timeout time.Duration
	var script string
	cmd := &cobra.Command{
		Use:   "send [text]",
		Short: "Send one message (or --script: a starter script) and print the reply",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			scripted := cmd.Flags().Changed("script")
			text, err := sendText(args, scripted)
			if err != nil {
				return err
			}

			cfg, name, err := sendConfig(profile)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}
			if scripted {
				return runScript(ctx, cfg, name, strings.TrimSpace(script), os.Stdout, os.Stderr)
			}
			return runSend(ctx, cfg, name, text, os.Stdout, os.Stderr)
		},
	}
	cmd.Flags().StringVarP(&profile, "profile", "p", "",
		"profile to send through (default: the active profile)")
	cmd.Flags().DurationVar(&timeout, "timeout", 0,
		"abort the request after this long (e.g. 90s; default: none)")
	cmd.Flags().StringVar(&script, "script", "",
		"play a starter script of the active prompt set (bare --script: the first; --script <name> picks)")
	cmd.Flags().Lookup("script").NoOptDefVal = " " // bare --script, distinct from unset
	return cmd
}

// sendText resolves what to send: the arguments, else piped stdin (echo hi |
// aibench send). A script plays a starter instead, so text and --script are
// mutually exclusive and one of them is required.
func sendText(args []string, scripted bool) (string, error) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" && !scripted {
		if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
			raw, _ := io.ReadAll(os.Stdin)
			text = strings.TrimSpace(string(raw))
		}
	}
	switch {
	case text == "" && !scripted:
		return "", errors.New("nothing to send: pass text as arguments or on stdin, or --script")
	case text != "" && scripted:
		return "", errors.New("--script plays a starter script; drop the text argument")
	}
	return text, nil
}

// sendConfig resolves the endpoint the way the TUI launch does: the
// named profile if one was asked for, else the settings file's active
// selection, with the profile's prompt set applied. It returns the
// resolved config and the profile name, the latter being what the
// summary line reports.
func sendConfig(profile string) (config.Config, string, error) {
	cfgFile, hasProfiles, err := config.LoadFile(config.FilePath())
	if err != nil {
		return config.Config{}, "", fmt.Errorf("config file error: %w", err)
	}
	if !hasProfiles {
		return config.Config{}, "", fmt.Errorf("no config file found (looked for %s)", config.FilePath())
	}
	name := profile
	if name == "" {
		name = prefs.ActiveProfile(cfgFile, prefs.Path(config.FilePath()))
	}
	cfg, err := cfgFile.Resolve(name)
	if err != nil {
		return config.Config{}, "", err
	}
	if err := cfgFile.ApplyPrompt(&cfg, config.ActivePromptSet(cfgFile, name)); err != nil {
		return config.Config{}, "", err
	}
	return cfg, name, nil
}

// runSend drives one request/response outside the TUI: the reply streamed
// to w, a summary line (and tool activity) to ew.
func runSend(ctx context.Context, cfg config.Config, profile, text string, w, ew io.Writer) error {
	return runConversation(ctx, cfg, profile,
		[]preset.Starter{{Text: text}}, w, ew)
}

// runConversation is the shared engine: build the client and prompt pair
// once, then one turn per message — user turns echoed with a "> " prefix
// when there is more than one (a script reads as a transcript).
func runConversation(ctx context.Context, cfg config.Config, profile string, script []preset.Starter, w, ew io.Writer) error {
	c, err := newConversation(cfg, profile)
	if err != nil {
		return err
	}
	// A script reads as a transcript, so its user turns are echoed; a
	// single message needs no echo of what you just typed.
	c.echo = len(script) > 1
	for _, msg := range script {
		if err := c.turn(ctx, msg.Text, w, ew); err != nil {
			return err
		}
	}
	return nil
}

// conversation is one CLI run: the client and prompt pair resolved once,
// and the store + state the turns accumulate into — the app's own, so
// the history each request carries is assembled by the same code.
type conversation struct {
	cfg     config.Config
	profile string
	st      *state.State
	sys     string
	params  provider.Params
	req     bindings.Set
	echo    bool
	msgs    chan tea.Msg // the pump's inbox: command results and captured HTTP events
}

// newConversation resolves everything a run needs before its first turn.
// The prompt pair rides along exactly as in the TUI — same file, same
// param parser, same tools file, no view involved; a promptless (kind:
// agent) profile sends the bare user message.
func newConversation(cfg config.Config, profile string) (*conversation, error) {
	captureCh := make(chan capture.Event, 64)
	client, err := provider.NewClient(cfg, captureCh)
	if err != nil {
		return nil, err
	}

	// A file the prompt set declared and the disk does not have is a
	// broken setup, and sending anyway would quietly drop the
	// instructions the run was supposed to test.
	var sys string
	var params provider.Params
	if cfg.PromptFile != "" {
		pre, err := preset.Load(cfg.PromptFile)
		if err != nil {
			return nil, fmt.Errorf("prompt file: %w", err)
		}
		sys = pre.System
		params = provider.SpecFor(cfg.API).ParseParams(pre.Params)
	}
	req, err := bindings.LoadAll(cfg.ToolsFiles)
	if err != nil {
		return nil, fmt.Errorf("tools file: %w", err)
	}

	c := &conversation{
		cfg: cfg, profile: profile, sys: sys, params: params, req: req,
		st:   state.New(store.New(64), client, captureCh, spinner.Model{}),
		msgs: make(chan tea.Msg, 64),
	}
	// Captured HTTP events feed the pump for the run's whole life (the
	// app arms a wait per event; one forwarder is the same thing here).
	go func() {
		for e := range captureCh {
			c.msgs <- state.CaptureMsg(e)
		}
	}()
	return c, nil
}

// turn sends one message and streams the reply to w, tool activity and the
// summary line to ew. The store keeps both, so the next turn carries them.
func (c *conversation) turn(ctx context.Context, text string, w, ew io.Writer) error {
	if c.echo {
		fmt.Fprintf(w, "> %s\n", text)
	}
	// The send spans its own context (esc in the app); ^C or --timeout
	// here reach it through Cancel, and the notice it leaves is the
	// answer.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			c.st.Cancel()
		case <-done: // this turn ended on its own; a later one is not ours to cancel
		}
	}()

	start := time.Now()
	var usage *provider.Usage
	printed := false
	c.pump(c.st.Send(c.sys, c.params, c.req, text), func(msg tea.Msg) tea.Cmd {
		switch msg := msg.(type) {
		case state.ResponseMsg:
			if msg.Content != "" {
				fmt.Fprint(w, msg.Content)
				printed = true
			}
			if msg.Usage != nil {
				usage = msg.Usage
			}
			for _, tc := range msg.ToolCalls {
				if !c.req.Bound(tc.Name) {
					fmt.Fprintf(ew, "tool call requested, not bound: %s(%s)\n", tc.Name, tc.Arguments)
				}
			}
			return c.st.Receive(provider.Delta(msg))
		case state.ToolResultMsg:
			if printed { // the follow-up reply starts on its own line
				fmt.Fprintln(w)
				printed = false
			}
			if msg.Err != nil {
				fmt.Fprintf(ew, "tool %s: error: %v\n", msg.Call.Name, msg.Err)
			} else {
				fmt.Fprintf(ew, "tool %s: %d bytes\n", msg.Call.Name, len(msg.Content))
			}
			return c.st.ToolResult(msg)
		case state.CaptureMsg:
			c.st.Capture(capture.Event(msg))
		}
		return nil // spinner ticks and the like: nothing to drive
	})
	if printed {
		fmt.Fprintln(w)
	}

	// A failed send ends on an error bubble, an interrupted one on a
	// notice; either is this run's exit status.
	if turns := c.st.Turns(); len(turns) > 0 {
		switch last := turns[len(turns)-1]; last.Role {
		case store.RoleError, store.RoleNotice:
			return errors.New(last.Content)
		}
	}
	fmt.Fprintln(ew, c.summary(start, usage))
	return nil
}

// pump is the message loop, headless: every command runs on its own
// goroutine and its message comes back here, where handle folds it into
// the state and returns the next command — exactly what Bubble Tea does
// for the app. It returns once the send is over. State is only ever
// touched on this goroutine.
func (c *conversation) pump(cmd tea.Cmd, handle func(tea.Msg) tea.Cmd) {
	run := func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		go func() { c.msgs <- cmd() }()
	}
	run(cmd)
	for c.st.Streaming() {
		msg := <-c.msgs
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, cmd := range batch {
				run(cmd)
			}
			continue
		}
		run(handle(msg))
	}
}

// summary is the one-line report every turn ends with: where it went, how
// long it took, and what it cost in tokens when the api said.
func (c *conversation) summary(start time.Time, usage *provider.Usage) string {
	target := c.cfg.Model
	if c.profile != "" {
		target = c.profile + " · " + c.cfg.Model
	}
	line := fmt.Sprintf("%s · %.1fs", target, time.Since(start).Seconds())
	if usage != nil {
		line += fmt.Sprintf(" · tokens in %d out %d", usage.Prompt, usage.Completion)
	}
	return line
}
