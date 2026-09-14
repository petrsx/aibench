// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package pricing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFetchPublished pins the published-table pull: the same YAML shape
// as the local file, parsed into the same rates.
func TestFetchPublished(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("gpt-5.4-mini:\n    input: 0.25\n    output: 2\n    context: 400000\n"))
	}))
	t.Cleanup(srv.Close)

	got, err := FetchPublished(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("FetchPublished() error = %v", err)
	}
	want := Rates{Input: 0.25, Output: 2, Context: 400000}
	if got["gpt-5.4-mini"] != want {
		t.Errorf("rates = %+v; want %+v", got["gpt-5.4-mini"], want)
	}

	// The env override wins over the built-in default, like every other
	// source knob here.
	t.Setenv("PRICING_URL", srv.URL)
	if got, err := FetchPublished(context.Background(), ""); err != nil || len(got) != 1 {
		t.Errorf("PRICING_URL override: table = %d entries, %v; want the served one", len(got), err)
	}

	// A server error is a named error, not an empty table.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(bad.Close)
	if _, err := FetchPublished(context.Background(), bad.URL); err == nil {
		t.Error("500 from the pricing host: FetchPublished returned nil error")
	}
}
