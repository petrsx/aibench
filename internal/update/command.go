// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package update

// How to install the newer build. Telling someone a release exists and
// leaving them to work out how to get it is half a message — but the
// answer depends on how they installed it, which nothing records. So it
// is inferred from what is on PATH, in the order the README offers the
// channels, and when nothing is recognised the caller falls back to the
// releases page rather than guessing a command that would fail.

import (
	"os/exec"
	"runtime"
)

// The published channels (see README): a Homebrew tap on macOS and Linux,
// winget on Windows, and the Go toolchain anywhere.
const (
	brewFormula = "petrsx/tap/aibench"
	wingetID    = "petrsx.aibench"
	goModule    = "github.com/petrsx/aibench@latest"
)

// lookPath is a var so tests can decide what is installed.
var lookPath = exec.LookPath

// UpgradeCommand is the command that updates this build, or "" when none
// of the known channels is available here. A package manager wins over
// the Go toolchain: a binary installed by brew or winget is the one that
// manager will replace, and `go install` would leave a second copy
// shadowing it.
func UpgradeCommand() string {
	if runtime.GOOS == "windows" {
		if have("winget") {
			return "winget upgrade " + wingetID
		}
		return goCommand()
	}
	if have("brew") {
		return "brew upgrade " + brewFormula
	}
	return goCommand()
}

func goCommand() string {
	if have("go") {
		return "go install " + goModule
	}
	return ""
}

func have(bin string) bool {
	_, err := lookPath(bin)
	return err == nil
}
