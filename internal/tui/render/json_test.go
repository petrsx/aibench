// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestColorizeJSONPreservesBytes(t *testing.T) {
	in := `{"a":"x: \"y\"","n":-1.5e3,"b":true,"c":null,"l":[false,2]}`
	if got := ansi.Strip(colorizeJSON(in)); got != in {
		t.Errorf("colorizeJSON changed the bytes:\n got %q\nwant %q", got, in)
	}
}
