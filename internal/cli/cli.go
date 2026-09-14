// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package cli is the command surface: the root command launches the TUI
// (the default, no-args behavior); `send` is the headless request, and
// the noun commands (`profiles`, `pricing`) hand their file to your
// editor or refresh it. `schema` exists for the build (`make schema`)
// and stays out of the help.
package cli

import (
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/petrsx/aibench/internal/app"
	"github.com/petrsx/aibench/internal/applog"
	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/prefs"
	"github.com/petrsx/aibench/internal/version"
)

// Execute runs the CLI; the build version comes from internal/version.
func Execute() {
	var cfgPath string
	var debug bool
	root := &cobra.Command{
		Use:           "aibench",
		Short:         "TUI for testing an AI endpoint, with live HTTP diagnostics",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// The flag pins the config before any command resolves it; without
		// it viper searches ./aibench.yaml → ./.aibench/ → ~/.aibench/.
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			if cfgPath != "" {
				config.SetFilePath(cfgPath)
			}
			// The app log (aibench.log beside the config). Writing it is
			// opt-in (settings.json "log"), and --debug turns it on for
			// this run whatever the setting says.
			settings := prefs.Load(prefs.Path(config.FilePath()))
			if _, err := applog.Init(settings.Log, debug); err != nil {
				printWarn("logging disabled: %v", err)
			}
			slog.Info("start", "version", version.Version, "command", cmd.Name(), "config", config.FilePath())
		},
		// No subcommand ⇒ launch the TUI (unchanged default behavior).
		RunE: func(*cobra.Command, []string) error { return app.Run() },
	}
	// --version carries the credit line — author and license — with the
	// build stamp, the same two lines the key sheet leads with.
	root.SetVersionTemplate(strings.Join(version.Notice(), "\n") + "\n")
	root.PersistentFlags().StringVar(&cfgPath, "config", "",
		"config file (default: ./aibench.yaml, ./.aibench/aibench.yaml, ~/.aibench/aibench.yaml)")
	root.PersistentFlags().BoolVar(&debug, "debug", false,
		"log at debug level (aibench.log beside the config file)")
	root.AddCommand(sendCmd(), profilesCmd(), pricingCmd(), schemaCmd())
	if err := root.Execute(); err != nil {
		printError(err)
		os.Exit(1)
	}
}
