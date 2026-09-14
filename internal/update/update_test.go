// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func stub(t *testing.T, tag string) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		fmt.Fprintf(w, `{"tag_name": %q}`, tag)
	}))
	t.Cleanup(srv.Close)
	prevURL, prevPath := releasesURL, cachePath
	releasesURL = srv.URL
	dir := t.TempDir()
	cachePath = func() string { return filepath.Join(dir, "update-check.json") }
	t.Cleanup(func() { releasesURL, cachePath = prevURL, prevPath })
	return &hits
}

// TestCheck pins the whole contract: dev builds never call out, a newer
// tag reports, the cache holds for the TTL, and a stale cache refreshes.
func TestCheck(t *testing.T) {
	hits := stub(t, "v1.2.0")

	if latest, newer := Check(t.Context(), "dev"); latest != "" || newer {
		t.Errorf("dev build = %q/%v; want silent no-op", latest, newer)
	}
	if hits.Load() != 0 {
		t.Fatal("dev build must not hit the network")
	}

	latest, newer := Check(t.Context(), "1.1.0")
	if latest != "1.2.0" || !newer {
		t.Fatalf("Check = %q/%v; want 1.2.0/newer", latest, newer)
	}
	// Second call: served from cache, no new hit.
	if _, newer := Check(t.Context(), "1.1.0"); !newer || hits.Load() != 1 {
		t.Errorf("cached check: newer=%v hits=%d; want true/1", newer, hits.Load())
	}
	// Up to date: same version is not newer.
	if _, newer := Check(t.Context(), "1.2.0"); newer {
		t.Error("same version reported as newer")
	}

	// Stale cache refreshes.
	raw, _ := json.Marshal(cache{CheckedAt: time.Now().Add(-48 * time.Hour), Latest: "1.2.0"})
	if err := os.WriteFile(cachePath(), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	Check(t.Context(), "1.1.0")
	if hits.Load() != 2 {
		t.Errorf("stale cache hits = %d; want a refresh call", hits.Load())
	}
}

// TestCheckFailuresAreSilent pins the quiet contract: a dead endpoint
// reports nothing and does not error.
func TestCheckFailuresAreSilent(t *testing.T) {
	prevURL, prevPath := releasesURL, cachePath
	releasesURL = "http://127.0.0.1:9/nope"
	dir := t.TempDir()
	cachePath = func() string { return filepath.Join(dir, "u.json") }
	t.Cleanup(func() { releasesURL, cachePath = prevURL, prevPath })

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if latest, newer := Check(ctx, "1.0.0"); latest != "" || newer {
		t.Errorf("dead endpoint = %q/%v; want silence", latest, newer)
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		cur, lat string
		want     bool
	}{
		{"1.0.0", "1.0.1", true},
		{"1.0.0", "1.0.0", false},
		{"1.2.0", "1.10.0", true}, // numeric, not lexicographic
		{"2.0.0", "1.9.9", false},
		{"v1.0.0", "1.0.1", true},
		{"1.0", "1.0.1", true},
		{"1.0.0", "", false},
	}
	for _, c := range cases {
		if got := isNewer(c.cur, c.lat); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v; want %v", c.cur, c.lat, got, c.want)
		}
	}
}
