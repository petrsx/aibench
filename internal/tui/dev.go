// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package tui

// Developer affordances: behaviour that exists for whoever is *building*
// aibench rather than whoever is using it. `AIBENCH_DEV=1` turns them on.
//
// One switch for all of them, not one variable each — a build either is a
// working copy or it is not. It is deliberately not `AIBENCH_DEBUG`:
// `--debug` already means "log at debug level" (aibench.log), and a
// second spelling of that word doing something else to the keyboard is the
// kind of ambiguity this tool exists to remove.
//
// Read once, at construction: a session cannot change shape halfway
// through, and nothing has to re-read the environment per keystroke.

import (
	"log/slog"
	"os"
	"strconv"
)

// devEnv is the switch's name, quoted wherever it is explained.
const devEnv = "AIBENCH_DEV"

// devMode reports whether the developer affordances are on. A value that
// is not a boolean is off and says so in the log — a knob that looks set
// and does nothing is the failure this codebase avoids everywhere else.
func devMode() bool {
	v := os.Getenv(devEnv)
	if v == "" {
		return false
	}
	on, err := strconv.ParseBool(v)
	if err != nil {
		slog.Warn("ignored: want true or false", "var", devEnv, "value", v)
		return false
	}
	return on
}
