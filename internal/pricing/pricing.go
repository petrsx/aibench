// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package pricing turns token usage into money and context-window
// pressure. Rates live in a local pricing.yaml discovered like the config
// and prompt files (PRICING_FILE → ./pricing.yaml → beside the config
// file → ~/.aibench/pricing.yaml) — never fetched at runtime; `aibench
// pricing update` refreshes it from catwalk. Keyed by model id, which on the
// azure host is the deployment name, so that's where those rows live.
package pricing

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"github.com/petrsx/aibench/internal/config"
)

// Rates is the price of one model: USD per 1M input/output tokens and the
// context window in tokens. A zero Context just hides the context row.
type Rates struct {
	Input   float64 `yaml:"input"`
	Output  float64 `yaml:"output"`
	Context int     `yaml:"context"`
}

// Cost is the USD for one exchange given its prompt/completion token counts.
func (r Rates) Cost(prompt, completion int) float64 {
	return float64(prompt)/1e6*r.Input + float64(completion)/1e6*r.Output
}

// FilePath is where the pricing file lives, found the same way as the
// config file (viper search, nearest wins): PRICING_FILE env → the
// current dir → ./.aibench/ → beside the resolved config file (a config
// pointed elsewhere via --config/CONFIG_FILE still gets its rates found
// next to it) → ~/.aibench/. When none exists it returns the config-dir
// path when a config file is present, else the user-level one (what
// `pricing update` creates by default).
func FilePath() string {
	if v := os.Getenv("PRICING_FILE"); v != "" {
		return v
	}
	const local = "pricing.yaml"
	v := viper.New()
	v.SetConfigName("pricing")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath(".aibench")
	cfgDir := ""
	if cfg := config.FilePath(); fileExists(cfg) {
		cfgDir = filepath.Dir(cfg)
		v.AddConfigPath(cfgDir)
	}
	if home, err := os.UserHomeDir(); err == nil {
		v.AddConfigPath(filepath.Join(home, ".aibench"))
	}
	if err := v.ReadInConfig(); err == nil {
		return v.ConfigFileUsed()
	}
	// Nothing exists yet: default the write target beside the config,
	// falling back to the user folder.
	if cfgDir != "" {
		return filepath.Join(cfgDir, local)
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".aibench", local)
	}
	return local
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Load reads a pricing file; a missing file is not an error (empty map).
func Load(path string) (map[string]Rates, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]Rates{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]Rates
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m == nil {
		m = map[string]Rates{}
	}
	return m, nil
}

// Save writes a pricing table to path atomically (temp + rename), creating
// the parent directory if needed. Keys serialize sorted for a stable diff.
func Save(path string, m map[string]Rates) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	out, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pricing-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(out); err != nil {
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

// Merge overlays over onto a copy of base; over wins per key.
func Merge(base, over map[string]Rates) map[string]Rates {
	out := make(map[string]Rates, len(base)+len(over))
	maps.Copy(out, base)
	maps.Copy(out, over)
	return out
}

// Table resolves a model id to its rates. The empty table simply never
// resolves, so cost rows stay hidden.
type Table map[string]Rates

// Lookup returns the rates for a model id.
func (t Table) Lookup(model string) (Rates, bool) {
	r, ok := t[model]
	return r, ok
}

// Runtime is the table the app reads: the pricing file at FilePath(), or an
// empty table when it's absent or unreadable (cost rows just stay hidden,
// like the app runs fine without a config or prompt file).
func Runtime() Table {
	m, err := Load(FilePath())
	if err != nil {
		return Table{}
	}
	return Table(m)
}
