// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package update

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

// TestUpgradeCommandFollowsWhatIsInstalled pins the inference: the
// command offered is one the machine can actually run. A package manager
// wins over the Go toolchain — a binary brew or winget installed is the
// one they will replace, and `go install` would leave a second copy
// shadowing it — and when nothing is recognised there is no command at
// all, so the caller can say "the releases page" instead of printing
// something that fails.
func TestUpgradeCommandFollowsWhatIsInstalled(t *testing.T) {
	installed := func(bins ...string) {
		t.Helper()
		old := lookPath
		t.Cleanup(func() { lookPath = old })
		lookPath = func(bin string) (string, error) {
			for _, b := range bins {
				if b == bin {
					return "/usr/bin/" + bin, nil
				}
			}
			return "", errors.New("not found")
		}
	}

	pm := "brew"
	if runtime.GOOS == "windows" {
		pm = "winget"
	}

	installed(pm, "go")
	if got := UpgradeCommand(); !strings.Contains(got, pm) {
		t.Errorf("with %s and go installed: %q; want the %s command", pm, got, pm)
	}
	installed("go")
	if got := UpgradeCommand(); !strings.HasPrefix(got, "go install ") {
		t.Errorf("with only go installed: %q; want the go command", got)
	}
	installed()
	if got := UpgradeCommand(); got != "" {
		t.Errorf("with nothing installed: %q; want no command at all", got)
	}
}
