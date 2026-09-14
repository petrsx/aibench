// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package config

// The prompts axis: named prompt sets, declared top-level and referenced
// by profiles. A profile says where requests go and, through its
// `prompt:` pin, what is sent with them — one axis, chosen once, so a
// run never depends on which set happened to be active. One set = one
// instructions file (markdown + YAML frontmatter params, hot-reloaded)
// plus 0..n starter scripts, each file one scripted conversation, plus
// 0..n tools references.

import (
	"fmt"
	"maps"
	"slices"
)

// PromptSet is one entry under the top-level `prompts:` map.
type PromptSet struct {
	// Instructions names the system-prompt file — the responses api's
	// term; on the chat/messages wires it lands as the system message.
	// Its frontmatter carries the request params. Empty is legal (a
	// starters-only set), and required for sets an agent profile pins.
	Instructions string `yaml:"instructions"`
	// Starters lists the starter-script files — each one scripted
	// conversation (## title per prompt; see preset.LoadStarters). A
	// list is the one spelling; a scalar fails the strict parse.
	Starters []string `yaml:"starters"`
	// Tools references the top-level tools map by key, in merge order —
	// a set can combine its own tool file with shared ones (websearch).
	Tools []string `yaml:"tools"`
}

// ApplyPrompt overlays the named set onto cfg: the instructions file and
// the starter scripts, both resolved relative to the yaml. It enforces
// the set-side invariants loudly: the name must exist, and an agent
// profile (cfg.Kind) must not pin a set carrying instructions — the
// agent owns those server-side (a starters-only set is fine).
func (f File) ApplyPrompt(cfg *Config, setName string) error {
	cfg.PromptFile, cfg.StartersFiles, cfg.ToolsFiles = "", nil, nil
	if setName == "" {
		return nil
	}
	set, ok := f.Prompts[setName]
	if !ok {
		return fmt.Errorf("prompt set %q not in config (prompts: %v)", setName, slices.Sorted(maps.Keys(f.Prompts)))
	}
	if cfg.Kind == KindAgent && set.Instructions != "" {
		return fmt.Errorf("prompt set %q: kind agent owns its instructions server-side — pin a starters-only set", setName)
	}
	if cfg.Kind == KindAgent && len(set.Tools) > 0 {
		return fmt.Errorf("prompt set %q: kind agent owns its tools server-side — pin a set without tools", setName)
	}
	// Every path in the config is taken as written and resolved against the
	// working directory — instructions, starters, tools, and the profile's
	// credentials file alike. A path therefore says what it would say typed
	// at a shell: run from the folder above and it is `project/prompts/x.md`,
	// run inside it and it is `prompts/x.md`. One rule, no exceptions; an
	// absolute path is always an absolute path.
	cfg.PromptFile = set.Instructions
	cfg.StartersFiles = append(cfg.StartersFiles, set.Starters...)
	for _, key := range set.Tools {
		path, ok := f.Tools[key]
		if !ok {
			return fmt.Errorf("prompt set %q: tools key %q not in config (tools: %v)", setName, key, slices.Sorted(maps.Keys(f.Tools)))
		}
		cfg.ToolsFiles = append(cfg.ToolsFiles, path)
	}
	return nil
}

// PromptFiles is every set's instructions and starter files, resolved —
// the launch path arms one watcher per file, so hot-reload keeps working
// after a profile switch brings a different set in.
func (f File) PromptFiles() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, name := range slices.Sorted(maps.Keys(f.Prompts)) {
		set := f.Prompts[name]
		add(set.Instructions)
		for _, s := range set.Starters {
			add(s)
		}
	}
	// Tool files too — shared keys dedup, so one watcher per file.
	for _, key := range slices.Sorted(maps.Keys(f.Tools)) {
		add(f.Tools[key])
	}
	return out
}

// ActivePromptSet is the set a profile runs with: its own pin when it
// has one, else the first set by name, else "" when none are declared.
// The set is a property of the profile — what is being sent is part of
// what an endpoint is being tested with — so nothing else gets a vote,
// and there is no remembered choice to override it. A stale pin does
// NOT fall through: an explicit reference to a missing set is a config
// error the profile switch reports loudly, not a preference.
func ActivePromptSet(f File, profileName string) string {
	if p, ok := f.Profiles[profileName]; ok && p.Prompt != "" {
		return p.Prompt
	}
	if len(f.Prompts) == 0 {
		return ""
	}
	return slices.Min(slices.Collect(maps.Keys(f.Prompts)))
}
