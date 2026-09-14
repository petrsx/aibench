// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package pricing

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestUpdate pins the merge both doors share: registry rows are added or
// refreshed, a row only the file has survives, and the summary counts
// each of those once.
func TestUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"models":[
			{"id":"gpt-4o","cost_per_1m_in":2.5,"cost_per_1m_out":10.0,"context_window":128000},
			{"id":"new-model","cost_per_1m_in":1.0,"cost_per_1m_out":2.0,"context_window":8192}
		]}]`))
	}))
	t.Cleanup(srv.Close)

	path := filepath.Join(t.TempDir(), "pricing.yaml")
	seed := "gpt-4o:\n    input: 1\n    output: 1\nmy-deployment:\n    input: 5\n    output: 5\n"
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Update(t.Context(), path, srv.URL)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	want := Summary{Path: path, Models: 3, Added: 1, Updated: 1, Kept: 1}
	if s != want {
		t.Errorf("Update() = %+v; want %+v", s, want)
	}
	if got := s.String(); got != "3 models (1 added, 1 updated, 1 kept)" {
		t.Errorf("String() = %q", got)
	}
	after, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if after["my-deployment"].Input != 5 {
		t.Error("the row only the file had did not survive the update")
	}
	if after["gpt-4o"].Input != 2.5 {
		t.Error("the registry's rate did not replace the stale one")
	}
}
