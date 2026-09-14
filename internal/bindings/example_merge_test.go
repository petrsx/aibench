// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package bindings

import "testing"

// TestExampleFilesMerge pins the shipped examples as a working pair: the
// weather set's own file plus the shared websearch file merge cleanly,
// with every tool of both bound or deliberately inspect-only.
func TestExampleFilesMerge(t *testing.T) {
	s, err := LoadAll([]string{
		"../../examples/prompts/weather-assistant.tools.json",
		"../../examples/prompts/websearch.tools.json",
	})
	if err != nil {
		t.Fatalf("shipped examples do not merge: %v", err)
	}
	for _, wire := range []string{"chat", "responses", "messages"} {
		fields, err := s.Overlay.Fields(wire)
		if err != nil || fields["tools"] == nil {
			t.Errorf("merged %s.tools missing (%v)", wire, err)
		}
	}
	for _, name := range []string{"search_locations", "get_weather", "websearch"} {
		if !s.Bound(name) {
			t.Errorf("example tool %s not bound after merge", name)
		}
	}
	if s.Bound("get_alerts") {
		t.Error("get_alerts should stay inspect-only")
	}
}
