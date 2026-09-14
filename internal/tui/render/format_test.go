// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

import (
	"testing"
	"time"
)

// TestElapsed pins the property the panel needs: precision that stays put
// as the magnitude changes, so a column of durations can be compared at a
// glance. Go's Duration.String() — what this replaced — switches unit and
// precision per value.
func TestElapsed(t *testing.T) {
	for in, want := range map[time.Duration]string{
		0:                                    "0 ms",
		396 * time.Millisecond:               "396 ms",
		999 * time.Millisecond:               "999 ms",
		time.Second:                          "1.00 s",
		1022 * time.Millisecond:              "1.02 s",
		3703 * time.Millisecond:              "3.70 s",
		9*time.Second + 999*time.Millisecond: "10.0 s", // rounds into the 1dp band
		12400 * time.Millisecond:             "12.4 s",
		time.Minute + 3*time.Second:          "1m 03s",
		-396 * time.Millisecond:              "-396 ms",
	} {
		if got := Elapsed(in); got != want {
			t.Errorf("Elapsed(%v) = %q; want %q", in, got, want)
		}
	}
}

// TestCost pins the two scales: a per-request cost is routinely a fraction
// of a cent, a session total is not.
func TestCost(t *testing.T) {
	for in, want := range map[float64]string{
		0:      "$0.0000",
		0.0121: "$0.0121",
		0.75:   "$0.7500",
		1:      "$1.00",
		1.05:   "$1.05",
		-1.05:  "$-1.05",
	} {
		if got := Cost(in); got != want {
			t.Errorf("Cost(%v) = %q; want %q", in, got, want)
		}
	}
}

// TestTokensSpellings pins that the three token spellings stay distinct
// and deliberate: exact where the number is compared, compact where the
// width is not there, and the axis's harder rounding where only the scale
// is read.
func TestTokensSpellings(t *testing.T) {
	const n = 1500
	for name, got := range map[string]string{
		"Tokens":        Tokens(n),
		"TokensCompact": TokensCompact(n),
		"TokensAxis":    TokensAxis(n),
	} {
		if got == "" {
			t.Errorf("%s(%d) = empty", name, n)
		}
	}
	if Tokens(n) != "1500" {
		t.Errorf("Tokens(1500) = %q; want the exact count", Tokens(n))
	}
	if TokensCompact(n) != "1.5k" {
		t.Errorf("TokensCompact(1500) = %q; want %q", TokensCompact(n), "1.5k")
	}
	if TokensAxis(n) != "2k" {
		t.Errorf("TokensAxis(1500) = %q; want the whole-unit axis form", TokensAxis(n))
	}
}

func TestBytes(t *testing.T) {
	for in, want := range map[int64]string{
		0:     "0 B",
		512:   "512 B",
		10603: "10.6 kB",
	} {
		if got := Bytes(in); got != want {
			t.Errorf("Bytes(%d) = %q; want %q", in, got, want)
		}
	}
}

func TestTimestamp(t *testing.T) {
	at := time.Date(2026, 8, 24, 2, 10, 44, 0, time.UTC)
	if got := Timestamp(at); got != "02:10:44" {
		t.Errorf("Timestamp() = %q; want %q", got, "02:10:44")
	}
}
