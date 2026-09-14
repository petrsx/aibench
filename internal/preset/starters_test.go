// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package preset

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStarters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assistant.starters.md")
	content := `# weather script
notes up here are ignored

## typo weather
whats the weather in pariis

## confirm
yes

## multi-line
line one
line two

## empty entry is dropped

`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	starters, err := LoadStarters(path)
	if err != nil {
		t.Fatalf("LoadStarters() error = %v", err)
	}
	want := []Starter{
		{Title: "typo weather", Text: "whats the weather in pariis"},
		{Title: "confirm", Text: "yes"},
		{Title: "multi-line", Text: "line one\nline two"},
	}
	if len(starters) != len(want) {
		t.Fatalf("got %d starters (%+v); want %d", len(starters), starters, len(want))
	}
	for i, w := range want {
		if starters[i] != w {
			t.Errorf("starters[%d] = %+v; want %+v", i, starters[i], w)
		}
	}
}

func TestLoadStartersMissing(t *testing.T) {
	// A declared file that is not there is an error; no path at all just
	// means the set declares no starters.
	if _, err := LoadStarters(filepath.Join(t.TempDir(), "none.md")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: err=%v; want fs.ErrNotExist", err)
	}
	if starters, err := LoadStarters(""); err != nil || starters != nil {
		t.Errorf("empty path: starters=%v err=%v; want nil, nil", starters, err)
	}
}
