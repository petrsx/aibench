// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package selection

import "testing"

func TestSelSpan(t *testing.T) {
	a, b := Pos{Line: 1, Col: 4}, Pos{Line: 3, Col: 2}
	tests := []struct {
		line     int
		from, to int
		ok       bool
	}{
		{line: 0, ok: false},
		{line: 1, from: 4, to: 10, ok: true},
		{line: 2, from: 0, to: 10, ok: true},
		{line: 3, from: 0, to: 3, ok: true},
		{line: 4, ok: false},
	}
	for _, tt := range tests {
		from, to, ok := Span(tt.line, a, b, 10)
		if ok != tt.ok || from != tt.from || to != tt.to {
			t.Errorf("Span(line %d) = %d,%d,%v; want %d,%d,%v",
				tt.line, from, to, ok, tt.from, tt.to, tt.ok)
		}
	}
}

func TestOrder(t *testing.T) {
	a, b := Order(Pos{Line: 5, Col: 2}, Pos{Line: 3, Col: 9})
	if a.Line != 3 || b.Line != 5 {
		t.Errorf("Order across lines = %v,%v; want line 3 first", a, b)
	}
	a, b = Order(Pos{Line: 4, Col: 8}, Pos{Line: 4, Col: 1})
	if a.Col != 1 || b.Col != 8 {
		t.Errorf("Order same line = %v,%v; want col 1 first", a, b)
	}
}
