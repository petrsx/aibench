// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestRunResult pins how the program's exit reaches the user: a crash is
// reportable, a normal teardown is silent. ^C must never print an error.
func TestRunResult(t *testing.T) {
	if err := runResult(nil); err != nil {
		t.Errorf("runResult(nil) = %v; want nil", err)
	}
	for _, quiet := range []error{tea.ErrProgramKilled, tea.ErrInterrupted} {
		if err := runResult(quiet); err != nil {
			t.Errorf("runResult(%v) = %v; want nil — a normal exit is not a failure", quiet, err)
		}
	}
	// A kill that wraps a panic is still a crash.
	err := runResult(tea.ErrProgramPanic)
	if err == nil {
		t.Fatal("runResult(ErrProgramPanic) = nil; want a reportable crash message")
	}
	for _, want := range []string{"crashed", "terminal has been restored", "aibench.log", "/issues"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("crash message missing %q; got:\n%v", want, err)
		}
	}
	// Anything else passes through unchanged, so its own message survives.
	other := errors.New("some other failure")
	if got := runResult(other); !errors.Is(got, other) {
		t.Errorf("runResult(%v) = %v; want the original error", other, got)
	}
}
