// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package fswatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchSignalsOnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(path, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ch := make(chan struct{}, 1)
	if err := Watch(t.Context(), path, ch); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	// A rename-replace (how editors save) must be seen too.
	tmp := filepath.Join(dir, "tmp.md")
	if err := os.WriteFile(tmp, []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("no watch signal after a rename-replace write")
	}
}
