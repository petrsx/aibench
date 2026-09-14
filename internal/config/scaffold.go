// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// TemplateYAML is the starter config Scaffold writes on first run: one
// placeholder model profile, no prompt files — the smallest thing a
// fresh install can edit into a working setup. Deliberately not named
// *.yaml so editors' YAML tooling leaves the template itself alone.
//
//go:embed config.yaml.tmpl
var TemplateYAML []byte

// schemaModeline is the yaml-language-server reference prepended at
// scaffold time, not kept in the template (a modeline in the template's
// own in-repo location would be redundant). Editor-only lint pointing at
// the published schema (SchemaURL) — the app maintains no schema copies
// on disk and never checks the line again.
const schemaModeline = "# yaml-language-server: $schema=" + SchemaURL + "\n"

// DefaultPath is where a fresh install gets its config: the user-level
// ~/.aibench/aibench.yaml (the last stop of the FilePath search, so the
// scaffolded file is found on the next run).
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".aibench", "aibench.yaml"), nil
}

// Pinned reports whether the config location was set explicitly
// (--config flag or the CONFIG_FILE env). A pinned-but-missing file is
// an error to report, never a first run to scaffold: a pinned path is
// not created behind anyone's back.
func Pinned() bool {
	return explicitPath != "" || os.Getenv("CONFIG_FILE") != ""
}

// Scaffold writes the starter config to path, creating its directory.
// It refuses to overwrite: an existing file is the user's, not ours.
func Scaffold(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(schemaModeline), TemplateYAML...), 0o644)
}
