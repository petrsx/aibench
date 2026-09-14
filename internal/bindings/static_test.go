// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package bindings

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/petrsx/aibench/internal/provider"
)

func TestStaticResultShapes(t *testing.T) {
	tests := []struct {
		name   string
		result string
		want   string
	}{
		{"object compacts", `{ "trains": [ "21:05" ] }`, `{"trains":["21:05"]}`},
		{"string unwraps", `"no trains tonight"`, "no trains tonight"},
		{"array compacts", `[ 1, 2 ]`, `[1,2]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Set{Bindings: map[string]Binding{
				"t": {Type: "static", Result: json.RawMessage(tt.result)},
			}}
			got, err := s.Run(t.Context(), provider.ToolCall{Name: "t"})
			if err != nil {
				t.Fatalf("Run() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Run() = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestStaticError(t *testing.T) {
	s := Set{Bindings: map[string]Binding{
		"t": {Type: "static", Error: "upstream timed out"},
	}}
	_, err := s.Run(t.Context(), provider.ToolCall{Name: "t"})
	if err == nil || !strings.Contains(err.Error(), "upstream timed out") {
		t.Errorf("Run() error = %v; want the canned error", err)
	}
}

func TestStaticDelayHonorsCancel(t *testing.T) {
	s := Set{Bindings: map[string]Binding{
		"t": {Type: "static", DelayMs: 60_000, Result: json.RawMessage(`{}`)},
	}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	start := time.Now()
	if _, err := s.Run(ctx, provider.ToolCall{Name: "t"}); err == nil {
		t.Error("Run(canceled) error = nil; want ctx error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Run(canceled) took %v; want an immediate return", elapsed)
	}
}

func TestRunUnknownBindingType(t *testing.T) {
	s := Set{Bindings: map[string]Binding{
		"t": {Type: "carrier-pigeon"},
	}}
	_, err := s.Run(t.Context(), provider.ToolCall{Name: "t"})
	if err == nil || !strings.Contains(err.Error(), "carrier-pigeon") {
		t.Errorf("Run(unknown type) error = %v; want it naming the bad type", err)
	}
}
