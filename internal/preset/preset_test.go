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

func TestLoadSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.md")
	in := Preset{
		System: "You are terse.\nAnswer in **one** sentence.",
		Params: map[string]string{
			"temperature": "0.3",
			"max_tokens":  "256",
			"custom_key":  "kept", // foreign keys survive
		},
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.System != in.System {
		t.Errorf("System = %q; want %q", got.System, in.System)
	}
	for k, want := range in.Params {
		if got.Params[k] != want {
			t.Errorf("Params[%s] = %q; want %q", k, got.Params[k], want)
		}
	}
}

func TestLoadCoercesYAMLValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.md")
	content := "---\ntemperature: 0.3\nmax_tokens: 256\nstop: END\n---\nBe brief.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Params["temperature"] != "0.3" || got.Params["max_tokens"] != "256" || got.Params["stop"] != "END" {
		t.Errorf("Params = %v; want stringified yaml values", got.Params)
	}
	if got.System != "Be brief." {
		t.Errorf("System = %q; want body", got.System)
	}
}

func TestLoadPlainMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(path, []byte("# Role\nJust a prompt, no frontmatter.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Params) != 0 {
		t.Errorf("Params = %v; want none", got.Params)
	}
	if got.System != "# Role\nJust a prompt, no frontmatter." {
		t.Errorf("System = %q; want full body", got.System)
	}
}

func TestLoadMissingFile(t *testing.T) {
	// The loader reports it rather than deciding for the caller: a prompt
	// set that declared this file is broken, and the caller says so.
	_, err := Load(filepath.Join(t.TempDir(), "nope.md"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Load(missing) error = %v; want fs.ErrNotExist", err)
	}
}
