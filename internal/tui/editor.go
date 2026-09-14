// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

// Editing your files from inside the app: aibench.yaml and pricing.yaml
// are human-owned — the app never writes the first and only appends to
// the second on request — but in a testing session they are the files
// that change most, so /profiles-edit and /pricing-edit hand them to an
// editor. A GUI editor (VS Code, Cursor, Zed, Sublime) is launched
// detached and the TUI keeps running: both files hot-reload, so every
// save lands through the same watcher path a change made in any other
// window takes, no waiting involved. A terminal editor needs the
// terminal, so the TUI suspends until it exits (tea.ExecProcess) and
// reloads the file on the way back rather than wait on the watcher.
//
// Which editor is a declaration, like everything else here: the
// settings file's "editor", else $VISUAL/$EDITOR. With none of them set
// the command opens the settings screen on the Editor row instead of
// dropping an unsuspecting user into vi — pick one there and it is
// remembered.

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/prefs"
	"github.com/petrsx/aibench/internal/pricing"
)

// editorDoneMsg carries a terminal editor's exit back into the message
// loop (detached GUI editors never produce one — the watcher does). path
// says which file, so the right reload follows.
type editorDoneMsg struct {
	path string
	err  error
}

// editorChoice is one offer on the settings screen: the binary that has
// to be on PATH, the command stored and run, and whether it detaches —
// a GUI editor runs alongside the TUI, a terminal one replaces it.
type editorChoice struct {
	bin    string
	cmd    string
	detach bool
}

// knownEditors are the editors the settings screen offers, in the order
// it rotates through them: the ones found on PATH. No wait flags — the
// GUI ones open detached and hot-reload carries the changes back.
var knownEditors = []editorChoice{
	{"code", "code", true},
	{"cursor", "cursor", true},
	{"zed", "zed", true},
	{"subl", "subl", true},
	{"nvim", "nvim", false},
	{"vim", "vim", false},
	{"hx", "hx", false},
	{"nano", "nano", false},
	{"micro", "micro", false},
	{"emacs", "emacs -nw", false},
	{"vi", "vi", false},
	{"notepad", "notepad", true},
}

// editConfig is /profiles-edit: aibench.yaml in the declared editor.
func (m *Model) editConfig() tea.Cmd {
	return m.editFile(config.FilePath())
}

// editPricing is /pricing-edit: pricing.yaml in the declared editor. The
// file may not exist yet — the editor creates it — but its folder must,
// or the editor has nowhere to save.
func (m *Model) editPricing() tea.Cmd {
	path := pricing.FilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return m.notifyError("pricing: " + err.Error())
	}
	return m.editFile(path)
}

// editFile hands one file to the declared editor, detached or suspended
// as the editor dictates.
func (m *Model) editFile(path string) tea.Cmd {
	argv := prefs.EditorCommand(m.prefs.Editor)
	if argv == nil {
		// Nothing declared anywhere: let them choose rather than land in
		// whatever the platform fallback happens to be.
		m.openSettings(rowEditor)
		return m.notifySay("pick an editor, then run the command again")
	}
	if detaches(argv) {
		return m.editDetached(argv, path)
	}
	// A terminal editor suspends the TUI — not while a reply streams: the
	// reload it comes back to is a profile switch, which mid-stream is a
	// no-op, so the edit would look applied and not be.
	if m.state.Streaming() {
		return m.notifyWarn(
			"not while a reply streams — esc interrupts it first")
	}
	return m.runEditor(argv, path)
}

// editDetached launches a GUI editor beside the TUI and returns to the
// conversation: the file's watcher applies every save as it lands. The
// wait goroutine only reaps the process — it never touches the model.
func (m *Model) editDetached(argv []string, path string) tea.Cmd {
	c := exec.Command(argv[0], append(argv[1:], path)...) //nolint:gosec // the user's own editor, declared or picked
	if err := c.Start(); err != nil {
		return m.notifyError("editor: " + err.Error())
	}
	go func() { _ = c.Wait() }()
	slog.Info("edit", "editor", strings.Join(argv, " "), "file", path, "detached", true)
	return m.notifySay("opened " + filepath.Base(path) + " — saves hot-reload")
}

// runEditor suspends the TUI on a terminal editor plus the file.
func (m *Model) runEditor(argv []string, path string) tea.Cmd {
	slog.Info("edit", "editor", strings.Join(argv, " "), "file", path)
	c := exec.Command(argv[0], append(argv[1:], path)...) //nolint:gosec // the user's own editor, declared or picked
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{path: path, err: err} })
}

// detaches reports whether this argv runs beside the TUI: a known GUI
// editor without an explicit wait flag. An explicit -w/--wait is the
// user asking to block, and an unknown editor gets the safe path — a
// suspend, which a non-blocking GUI editor returns from immediately.
func detaches(argv []string) bool {
	if slices.Contains(argv[1:], "-w") || slices.Contains(argv[1:], "--wait") {
		return false
	}
	base := strings.TrimSuffix(filepath.Base(argv[0]), ".exe")
	for _, e := range knownEditors {
		if e.bin == base {
			return e.detach
		}
	}
	return false
}

// installedEditors filters knownEditors down to what is on PATH — the
// values the settings screen's Editor row rotates through.
func installedEditors() []editorChoice {
	var found []editorChoice
	for _, e := range knownEditors {
		if _, err := exec.LookPath(e.bin); err == nil {
			found = append(found, e)
		}
	}
	return found
}
