// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package applog

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitWritesAndRotates pins the log lifecycle: Init points slog at
// the file, entries land, and an oversized file rotates to .old on the
// next Init.
func TestInitWritesAndRotates(t *testing.T) {
	// The log lives beside the config, so pinning the config is what
	// puts this test's log somewhere disposable.
	home := t.TempDir()
	t.Setenv("CONFIG_FILE", filepath.Join(home, "aibench.yaml"))
	// Windows cannot delete an open file: release the handle so the
	// TempDir cleanup succeeds (slog's default keeps pointing at the
	// closed file; its write errors are slog's to swallow).
	t.Cleanup(Close)

	path, err := Init(true, true)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	slog.Info("hello", "k", "v")
	slog.Debug("dbg") // debug=true: must land too
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "hello") || !strings.Contains(string(raw), "dbg") {
		t.Errorf("log = %q; want info and debug entries", raw)
	}

	// Blow past the size guard and re-Init: the old file rotates away.
	if err := os.WriteFile(path, make([]byte, maxSize+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(true, false); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() > 1024 {
		t.Errorf("post-rotation size = %v, %v; want a fresh file", info, err)
	}
	if _, err := os.Stat(path + ".old"); err != nil {
		t.Errorf("rotated generation missing: %v", err)
	}
}

// TestInitOffWritesNothing pins the default: with logging unasked for,
// no file is opened and nothing is recorded — a tool that accumulates a
// file about your endpoints has to be asked.
func TestInitOffWritesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONFIG_FILE", filepath.Join(dir, "aibench.yaml"))

	path, err := Init(false, false)
	if err != nil {
		t.Fatalf("Init(off) error = %v", err)
	}
	if path != "" {
		t.Errorf("Init(off) = %q; want no path — nothing should be opened", path)
	}
	slog.Error("this must not be written anywhere")
	if _, err := os.Stat(Path()); !os.IsNotExist(err) {
		t.Errorf("a log file exists at %s with logging off", Path())
	}
}
