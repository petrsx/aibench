// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package config

import "testing"

// TestActivePromptSet pins the rule: a profile runs with the set it pins
// (verbatim — a stale pin surfaces at ApplyPrompt, loudly), else the
// first set by name, else none. Nothing remembered gets a vote: the set
// is a property of the profile, so the same profile always resolves to
// the same prompt.
func TestActivePromptSet(t *testing.T) {
	f := File{
		Prompts: map[string]PromptSet{
			"bravo": {Instructions: "b.md"},
			"alpha": {Instructions: "a.md"},
		},
		Profiles: map[string]Profile{
			"pinned":   {Kind: KindModel, Prompt: "bravo"},
			"unpinned": {Kind: KindModel},
		},
	}

	if got := ActivePromptSet(f, "pinned"); got != "bravo" {
		t.Errorf("pinned profile: set = %q; want the pin bravo", got)
	}
	if got := ActivePromptSet(f, "unpinned"); got != "alpha" {
		t.Errorf("unpinned profile: set = %q; want first-by-name alpha", got)
	}
	// Repeating a resolve cannot drift — there is no history behind it.
	if got := ActivePromptSet(f, "unpinned"); got != "alpha" {
		t.Errorf("second resolve: set = %q; want alpha again", got)
	}
	if got := ActivePromptSet(File{Profiles: f.Profiles}, "unpinned"); got != "" {
		t.Errorf("no sets: set = %q; want empty", got)
	}
}
