// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestScaffoldWritesTemplate pins the write path: directory created,
// schema modeline prepended (its path only resolves beside the written
// config, so the template itself doesn't carry it), template following
// byte-for-byte, an existing file refused.
func TestScaffoldWritesTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".aibench", "aibench.yaml")
	if err := Scaffold(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, append([]byte(schemaModeline), TemplateYAML...)) {
		t.Fatalf("scaffolded file differs from modeline + template: %v", err)
	}
	if bytes.Contains(TemplateYAML, []byte("yaml-language-server")) {
		t.Error("template carries the schema modeline; it must be prepended at scaffold time only")
	}
	if err := Scaffold(path); err == nil {
		t.Error("second Scaffold overwrote an existing config; want refusal")
	}
}

// TestTemplateParsesAndValidates keeps the embedded starter config
// honest: it must strict-parse, carry exactly one placeholder profile
// with no prompt group, and validate against the JSON Schema.
func TestTemplateParsesAndValidates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aibench.yaml")
	if err := os.WriteFile(path, TemplateYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	f, ok, err := LoadFile(path)
	if err != nil || !ok {
		t.Fatalf("template does not strict-parse: %v", err)
	}
	if len(f.Profiles) != 1 {
		t.Fatalf("template has %d profiles; want the single starter", len(f.Profiles))
	}
	p, ok := f.Profiles["dev"]
	if !ok {
		t.Fatal("template's starter profile is not named dev")
	}
	if p.Prompt != "" {
		t.Error("template pins a prompt set; the starter config must reference none")
	}

	if err := validateYAML(t, compiled(t), TemplateYAML); err != nil {
		t.Errorf("template does not validate against the schema: %v", err)
	}
}
