// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package version holds the build version, one leaf every layer may
// read — the CLI's --version, the TUI header credit, the update check.
package version

// Version is stamped by goreleaser at release time (ldflags -X); "dev"
// in local builds.
var Version = "dev"

// The identity behind the credit line: the copyright holder, the SPDX
// identifier the source headers carry, and where the license is read.
const (
	Copyright  = "Copyright (C) 2026 Petr Stupka"
	License    = "MIT"
	LicenseURL = "https://github.com/petrsx/aibench/blob/main/LICENSE"
)

// Notice is the credit shown by --version and at the top of the key
// sheet: the build, who wrote it, and the license it comes under. Two
// short unstyled lines, for a caller to join or render, so it sits above
// the keymap without pushing it off the pane.
func Notice() []string {
	return []string{
		"aibench " + Version + "  " + Copyright,
		License + " license — " + LicenseURL,
	}
}
