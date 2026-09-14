// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/prefs"
)

// The `edit` verbs (`profiles edit`, `pricing edit`) hand one of the
// files aibench reads to the declared editor and wait for it: the
// settings file's "editor", else $VISUAL/$EDITOR — the same declaration
// /profiles-edit reads in the app (prefs.EditorCommand). Nothing is declared ⇒ a
// named error rather than a guess; a GUI editor that returns at once is
// fine, the command simply ends. The file need not exist yet: the editor
// creates it, which is how a first pricing.yaml or aibench.yaml comes to
// be by hand.

// editCmd builds one `edit` subcommand for the file path returns.
func editCmd(what string, path func() string) *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open " + what + " in your editor",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings := prefs.Load(prefs.Path(config.FilePath()))
			return runEditor(settings.Editor, path())
		},
	}
}

// runEditor runs the declared editor on path, attached to this terminal.
func runEditor(pref, path string) error {
	argv := prefs.EditorCommand(pref)
	if argv == nil {
		return errors.New(`no editor declared: set "editor" in settings.json, or $VISUAL / $EDITOR`)
	}
	c := exec.Command(argv[0], append(argv[1:], path)...) //nolint:gosec // the user's own editor, declared
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}
	return nil
}
