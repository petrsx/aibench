// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/petrsx/aibench/internal/config"
)

// schemaCmd prints the embedded JSON Schema for aibench.yaml. Hidden: it
// is how `make schema` regenerates the published schema/aibench.schema.json
// from the binary's copy, not a user-facing surface — editors read the
// published URL.
func schemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "schema",
		Short:  "Print the JSON Schema for aibench.yaml",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprint(cmd.OutOrStdout(), string(config.SchemaJSON))
			return nil
		},
	}
}
