// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package preset loads and saves the prompt file: one markdown document
// with YAML frontmatter — sampling params in the frontmatter, the system
// prompt as the body. The file is the source of truth: the user edits it
// in their editor and the TUI hot-reloads it; TUI edits save back. The
// file is the preset; switching between several files is a later feature.
package preset

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const fence = "---"

// Preset is the prompt file's content: raw param strings keyed by the
// spec param names (foreign keys survive round-trips), and the system
// prompt body.
type Preset struct {
	System string
	Params map[string]string
}

// Load parses the prompt file. A missing file comes back as fs.ErrNotExist
// rather than an empty preset: a prompt set that declares instructions
// and cannot find them is a broken setup, and the caller says so —
// swallowing it here would hide that behind an empty prompt.
//
// Frontmatter is optional: a plain markdown file is all system prompt.
func Load(path string) (Preset, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Preset{}, err
	}

	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	p := Preset{Params: map[string]string{}}

	body := text
	if after, ok := strings.CutPrefix(text, fence+"\n"); ok {
		if front, rest, ok := strings.Cut(after, "\n"+fence+"\n"); ok {
			var values map[string]any
			if err := yaml.Unmarshal([]byte(front), &values); err != nil {
				return Preset{}, fmt.Errorf("frontmatter: %w", err)
			}
			for k, v := range values {
				p.Params[k] = fmt.Sprintf("%v", v)
			}
			body = rest
		}
	}
	p.System = strings.TrimSpace(body)
	return p, nil
}

// Save writes the preset atomically (temp file + rename), canonical
// frontmatter first (keys sorted, values as strings), body after.
func Save(path string, p Preset) error {
	var b strings.Builder
	if len(p.Params) > 0 {
		keys := make([]string, 0, len(p.Params))
		for k := range p.Params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString(fence + "\n")
		for _, k := range keys {
			out, err := yaml.Marshal(map[string]string{k: p.Params[k]})
			if err != nil {
				return err
			}
			b.Write(out)
		}
		b.WriteString(fence + "\n")
	}
	b.WriteString(p.System)
	if p.System != "" {
		b.WriteString("\n")
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".prompt-*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
