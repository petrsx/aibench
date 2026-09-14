// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestNiceCeil(t *testing.T) {
	for _, tt := range []struct {
		in, want float64
	}{
		{0, 10},
		{8, 10},
		{19, 20},
		{48, 50},
		{50, 50},
		{73, 100},
		{101, 200},
		{350, 500},
		{800, 1000},
		{1200, 2000},
	} {
		if got := NiceCeil(tt.in); got != tt.want {
			t.Errorf("NiceCeil(%v) = %v; want %v", tt.in, got, tt.want)
		}
	}
}

func TestIsJSONContent(t *testing.T) {
	cases := map[string]bool{
		`{"a":1}`:          true,
		"  [1, 2, 3]  ":    true,
		"hello world":      false,
		"{not json":        false,
		"":                 false,
		`{"a":1} trailing`: false, // trailing data ⇒ not a single value
		"> quoted prose":   false,
	}
	for in, want := range cases {
		if got := IsJSONContent(in); got != want {
			t.Errorf("IsJSONContent(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestPrettyBody(t *testing.T) {
	// Assertions strip styling first: lipgloss v2 emits colors regardless of
	// TTY (downsampling moved into the Bubble Tea renderer).
	got := PrettyBody(`{"model":"m","messages":[{"role":"user"}]}`)
	joined := ansi.Strip(strings.Join(got, "\n"))
	if len(got) < 5 || !strings.Contains(joined, `  "messages": [`) {
		t.Errorf("PrettyBody(json) not indented:\n%s", joined)
	}

	// SSE: each data payload pretty-prints; [DONE] and separators stay.
	got = PrettyBody("data: {\"a\":1}\n\ndata: [DONE]")
	want := []string{"data: {", `  "a": 1`, "}", "", "data: [DONE]"}
	if ansi.Strip(strings.Join(got, "\n")) != strings.Join(want, "\n") {
		t.Errorf("PrettyBody(sse) = %q; want %q", got, want)
	}

	if got := PrettyBody("{not json"); ansi.Strip(got[0]) != "{not json" {
		t.Errorf("PrettyBody(invalid) = %q; want verbatim", got[0])
	}

	// A final event cut mid-JSON by the 8KB body cap collapses to a compact
	// "(truncated)" marker instead of dumping the broken line.
	got = PrettyBody("data: {\"a\":1}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"partial")
	j := ansi.Strip(strings.Join(got, "\n"))
	if !strings.Contains(j, "(truncated)") || strings.Contains(j, `"content": "partial`) {
		t.Errorf("truncated SSE event not collapsed to a marker:\n%s", j)
	}
}
