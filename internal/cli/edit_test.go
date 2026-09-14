// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRunEditor pins the edit verbs' contract: the declared editor runs
// with the file as its last argument, its flags kept; nothing declared
// is a named error, not a fallback editor.
func TestRunEditor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake editor is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "argv")
	fake := filepath.Join(dir, "ed")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + log + "\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil { //nolint:gosec // an executable fixture
		t.Fatal(err)
	}

	if err := runEditor(fake+" --flag", "/tmp/aibench.yaml"); err != nil {
		t.Fatalf("runEditor error = %v", err)
	}
	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if want := "--flag\n/tmp/aibench.yaml\n"; string(got) != want {
		t.Errorf("editor argv = %q; want %q", got, want)
	}

	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	err = runEditor("", "/tmp/aibench.yaml")
	if err == nil || !strings.Contains(err.Error(), "no editor declared") {
		t.Errorf("nothing declared: error = %v; want a named one", err)
	}
}
