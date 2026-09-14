// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestProfilesListCmd(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "aibench.yaml")
	yaml := "profiles:\n" +
		"  bravo:\n    kind: model\n    api:\n      provider: openai\n      base: https://b\n    auth:\n      credentials: env\n" +
		"  alpha:\n    kind: model\n    api:\n      provider: openai\n      base: https://a\n    auth:\n      credentials: env\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", cfgPath)

	cmd := profilesListCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("profiles list error = %v", err)
	}
	if got := out.String(); got != "alpha\nbravo\n" {
		t.Errorf("output = %q; want sorted plain names", got)
	}

	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "none.yaml"))
	if err := profilesListCmd().Execute(); err == nil {
		t.Error("no config: want an error, got nil")
	}
}
