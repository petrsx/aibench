// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package pricing

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestRuntimeReadsPricingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pricing.yaml")
	if err := Save(path, map[string]Rates{"gpt-4o": {Input: 2.5, Output: 10, Context: 128000}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PRICING_FILE", path)

	if got := FilePath(); got != path {
		t.Errorf("FilePath() = %q; want PRICING_FILE %q", got, path)
	}
	r, ok := Runtime().Lookup("gpt-4o")
	if !ok || r.Input != 2.5 || r.Context != 128000 {
		t.Errorf("Runtime lookup = %+v, %v; want the file's gpt-4o row", r, ok)
	}
}

func TestRuntimeMissingIsEmpty(t *testing.T) {
	t.Setenv("PRICING_FILE", filepath.Join(t.TempDir(), "none.yaml"))
	if _, ok := Runtime().Lookup("gpt-4o"); ok {
		t.Error("Runtime with no file resolved a model; want empty table")
	}
}

func TestCost(t *testing.T) {
	r := Rates{Input: 3, Output: 15}
	// 1M prompt @3 + 0.5M completion @15 = 3 + 7.5
	if got := r.Cost(1_000_000, 500_000); got != 10.5 {
		t.Errorf("Cost = %v; want 10.5", got)
	}
	if got := (Rates{}).Cost(100, 100); got != 0 {
		t.Errorf("zero rates Cost = %v; want 0", got)
	}
}

func TestMergeOverWins(t *testing.T) {
	base := map[string]Rates{"a": {Input: 1}, "b": {Input: 2}}
	over := map[string]Rates{"b": {Input: 20}, "c": {Input: 3}}
	got := Merge(base, over)
	if got["a"].Input != 1 || got["b"].Input != 20 || got["c"].Input != 3 {
		t.Errorf("Merge = %+v; want a=1 b=20 c=3", got)
	}
	if base["b"].Input != 2 {
		t.Error("Merge mutated base")
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "pricing.yaml")
	want := map[string]Rates{"m": {Input: 1.5, Output: 6, Context: 4096}}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got["m"] != want["m"] {
		t.Errorf("roundtrip = %+v; want %+v", got, want)
	}
}

func TestLoadMissing(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "none.yaml"))
	if err != nil || len(got) != 0 {
		t.Errorf("Load(missing) = %v, %v; want empty, nil", got, err)
	}
}

func TestFetchCatwalk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/providers" {
			t.Errorf("path = %q; want /v2/providers", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"models":[
				{"id":"claude-sonnet-4-6","cost_per_1m_in":3.0,"cost_per_1m_out":15.0,"context_window":200000},
				{"id":"free-model","cost_per_1m_in":0,"cost_per_1m_out":0,"context_window":8192}
			]},
			{"models":[
				{"id":"gpt-4o","cost_per_1m_in":2.5,"cost_per_1m_out":10.0,"context_window":128000}
			]}
		]`))
	}))
	t.Cleanup(srv.Close)

	got, err := FetchCatwalk(t.Context(), srv.URL)
	if err != nil {
		t.Fatalf("FetchCatwalk() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows; want 2 (zero-cost skipped): %+v", len(got), got)
	}
	if r := got["claude-sonnet-4-6"]; r.Input != 3 || r.Output != 15 || r.Context != 200000 {
		t.Errorf("claude row = %+v; want {3 15 200000}", r)
	}
	if _, ok := got["free-model"]; ok {
		t.Error("free-model included; want zero-cost rows skipped")
	}
}
