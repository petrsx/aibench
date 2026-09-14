// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package cli

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/spf13/cobra"

	"github.com/petrsx/aibench/internal/config"
)

// The profiles command tree: the config file's `profiles:` axis. Bare
// `profiles` prints its verbs (cobra's help for a parent without a run);
// `edit` is the one on offer, `list` stays hidden for the acceptance
// harness, which iterates it.

func profilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "Manage the config file's profiles (aibench.yaml)",
	}
	cmd.AddCommand(
		editCmd("aibench.yaml", config.FilePath),
		profilesListCmd(),
	)
	return cmd
}

// profilesListCmd prints one profile name per line — deliberately plain
// so scripts can iterate it. Hidden: a script's tool, not a user's.
func profilesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "list",
		Short:  "List the config file's profiles, one per line",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, ok, err := config.LoadFile(config.FilePath())
			if err != nil {
				return err
			}
			if !ok || len(f.Profiles) == 0 {
				return errors.New("no profiles: no config file found")
			}
			for _, name := range slices.Sorted(maps.Keys(f.Profiles)) {
				fmt.Fprintln(cmd.OutOrStdout(), name)
			}
			return nil
		},
	}
}
