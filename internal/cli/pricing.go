// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package cli

import (
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/petrsx/aibench/internal/pricing"
	"github.com/petrsx/aibench/internal/tui/theme"
)

// The pricing command tree: the pricing.yaml the Metrics panel reads —
// refresh it from catwalk, or open it to add your own rows. Named for
// the file and the package it manages — "cost" is what the panel *shows*,
// one row of it.

func pricingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pricing",
		Short: "Manage model pricing (pricing.yaml)",
	}
	cmd.AddCommand(pricingUpdateCmd(), editCmd("pricing.yaml", pricing.FilePath))
	return cmd
}

func pricingUpdateCmd() *cobra.Command {
	var out, url string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Refresh pricing.yaml from catwalk (your own rows are preserved)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// catwalk is the source, as it is for the launch fetch: one
			// public registry, no copy of it to keep current.
			s, err := pricing.Update(cmd.Context(), out, url)
			if err != nil {
				return err
			}
			// lipgloss.Println downsamples for the terminal and strips color
			// when piped (the v2 non-TUI output path).
			lipgloss.Println(theme.NoticeStyle.Render("pricing:") + " " + s.String() + " " +
				theme.DimStyle.Render("→ "+s.Path))
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "file to write (default: PRICING_FILE / ./pricing.yaml / ~/.aibench/pricing.yaml)")
	cmd.Flags().StringVar(&url, "url", "", "source URL (default: CATWALK_URL, else catwalk.charm.land)")
	return cmd
}
