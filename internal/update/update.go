// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package update checks GitHub for a newer release — quietly: at most
// one network call a day (cached), a 3s budget, and every failure
// silent-but-logged. Dev builds never check.
package update

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// releasesURL is a var so tests can point it at a stub.
var releasesURL = "https://api.github.com/repos/petrsx/aibench/releases/latest"

const cacheTTL = 24 * time.Hour

type cache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

// cachePath is a var so tests can redirect it.
var cachePath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "aibench-update-check.json")
	}
	return filepath.Join(home, ".aibench", "update-check.json")
}

// Check reports the latest released version and whether it is newer than
// current. A "dev" build, a fresh cache, or any failure reports nothing.
func Check(ctx context.Context, current string) (latest string, newer bool) {
	if current == "" || current == "dev" {
		return "", false
	}
	path := cachePath()
	if c, err := readCache(path); err == nil && time.Since(c.CheckedAt) < cacheTTL {
		return c.Latest, isNewer(current, c.Latest)
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return "", false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", false
	}
	latest = strings.TrimPrefix(rel.TagName, "v")
	_ = writeCache(path, cache{CheckedAt: time.Now(), Latest: latest})
	return latest, isNewer(current, latest)
}

func readCache(path string) (cache, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return cache{}, err
	}
	var c cache
	return c, json.Unmarshal(raw, &c)
}

func writeCache(path string, c cache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// isNewer compares dotted numeric versions ("1.2.3"), segment by
// segment; anything unparsable compares as zero.
func isNewer(current, latest string) bool {
	if latest == "" || latest == strings.TrimPrefix(current, "v") {
		return false
	}
	cur := segments(strings.TrimPrefix(current, "v"))
	lat := segments(latest)
	for i := range max(len(cur), len(lat)) {
		c, l := 0, 0
		if i < len(cur) {
			c = cur[i]
		}
		if i < len(lat) {
			l = lat[i]
		}
		if l != c {
			return l > c
		}
	}
	return false
}

func segments(v string) []int {
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out
}
