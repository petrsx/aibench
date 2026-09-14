// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/petrsx/aibench/internal/config"
	"github.com/petrsx/aibench/internal/preset"
)

// Script selection for `send --script`: pick one of the active prompt
// set's starter scripts by display name and hand it to the shared
// conversation engine in send.go.

// runScript plays the profile's starter prompts in order, each sent
// after the previous reply completed, carrying the conversation history
// (and the stored-response chain, when the profile uses one) exactly as
// consecutive TUI sends would.
func runScript(ctx context.Context, cfg config.Config, profile, script string, w, ew io.Writer) error {
	if len(cfg.StartersFiles) == 0 {
		return errors.New("no script: the active prompt set declares no starters")
	}
	path := cfg.StartersFiles[0] // bare --script: the first script, the launch offer's default
	if script != "" {
		path = ""
		for _, p := range cfg.StartersFiles {
			if scriptDisplayName(p) == script {
				path = p
				break
			}
		}
		if path == "" {
			names := make([]string, len(cfg.StartersFiles))
			for i, p := range cfg.StartersFiles {
				names[i] = scriptDisplayName(p)
			}
			return fmt.Errorf("no script named %q (have: %s)", script, strings.Join(names, ", "))
		}
	}
	starters, err := preset.LoadStarters(path)
	if err != nil {
		return fmt.Errorf("starters file: %w", err)
	}
	if len(starters) == 0 {
		return fmt.Errorf("no script: %s has no starter prompts", path)
	}
	return runConversation(ctx, cfg, profile, starters, w, ew)
}

// scriptDisplayName mirrors the TUI's script naming: the basename with
// the conventional suffixes stripped (weather.starters.md → weather).
func scriptDisplayName(path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".md")
	name = strings.TrimSuffix(name, ".starters")
	return name
}
