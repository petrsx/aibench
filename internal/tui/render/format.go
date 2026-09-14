// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package render

// How a value is written, by what kind of value it is. Every surface —
// the Metrics panel, the record lines, the Inspector's head and size pane
// — formats through this file, so the same quantity never reads two ways
// on one screen. Before it existed there were nine formatters in three
// packages and four spellings of a token count.
//
// Each name says the kind, not the shape: Elapsed is how long, Timestamp
// is when, and the two used to collide under one "time" row.

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Timestamp is a wall-clock instant, to the second — when something
// happened, never how long it took.
func Timestamp(t time.Time) string { return t.Format("15:04:05") }

// Elapsed is how long something took, at a precision that stays stable
// down a column: milliseconds below a second, then two significant
// decimals, then whole seconds, then minutes. Go's own Duration.String()
// is what this replaces — it switches unit *and* precision per value
// ("396ms" over "1.022s" over "9s"), so a column of them cannot be
// compared at a glance, and it implies microsecond precision that a
// network measurement does not have.
func Elapsed(d time.Duration) string {
	// The band is chosen after rounding, not before: 9.999s belongs to the
	// one-decimal band ("10.0s"), because "10.00s" would be the only value
	// in the column a digit wider than its neighbours.
	switch secs := d.Seconds(); {
	case d < 0:
		return "-" + Elapsed(-d)
	case d < time.Second:
		return strconv.Itoa(int(d.Round(time.Millisecond)/time.Millisecond)) + " ms"
	case secs < 9.995:
		return strconv.FormatFloat(secs, 'f', 2, 64) + " s"
	case d < time.Minute:
		return strconv.FormatFloat(secs, 'f', 1, 64) + " s"
	default:
		m := int(d / time.Minute)
		s := int((d % time.Minute).Round(time.Second) / time.Second)
		return fmt.Sprintf("%dm %02ds", m, s)
	}
}

// Cost is money, in the currency the pricing table quotes (USD): two
// decimals at cents scale, four below a cent, because a per-request cost
// is routinely a fraction of one. The scales differ by design — a row
// reading $0.0121 next to one reading $1.05 is the honest rendering of
// two numbers three orders apart.
func Cost(v float64) string {
	if math.Abs(v) >= 1 {
		return fmt.Sprintf("$%.2f", v)
	}
	return fmt.Sprintf("$%.4f", v)
}

// Tokens is an exact count, for the panels where the number is the point
// and you compare it against the one below it.
func Tokens(n int) string { return strconv.Itoa(n) }

// TokensCompact abbreviates a count where the width is not there to spend
// — a record line, the context row: 1000 → "1k", 1500 → "1.5k",
// 1000000 → "1M". One decimal at most, and a trailing ".0" is dropped so
// round numbers stay short.
func TokensCompact(n int) string {
	switch {
	case n < 0:
		return "-" + TokensCompact(-n)
	case n < 1000:
		return strconv.Itoa(n)
	case n < 1_000_000:
		return trimUnit(float64(n)/1000, "k")
	default:
		return trimUnit(float64(n)/1_000_000, "M")
	}
}

// TokensAxis abbreviates a count for the Tokens graph's axis, built to
// never exceed three cells for any value the scale can take (NiceCeil's
// 1/2/5 steps and their halves, up to double-digit millions): at most two
// digits and a unit (2500 → "3k"), with the 100k–999k band as tenths of a
// meg, leading zero dropped (400000 → ".4M") — the axis is read for scale
// alone, so its labels round harder than anywhere else. That is why it is
// its own function and not TokensCompact: the exception is deliberate.
func TokensAxis(n int) string {
	switch {
	case n < 0:
		return "-" + TokensAxis(-n)
	case n < 1000:
		return strconv.Itoa(n)
	}
	if k := int(math.Round(float64(n) / 1000)); k < 100 {
		return strconv.Itoa(k) + "k"
	}
	if tenths := int(math.Round(float64(n) / 100_000)); tenths < 10 {
		return "." + strconv.Itoa(tenths) + "M"
	}
	return strconv.Itoa(int(math.Round(float64(n)/1_000_000))) + "M"
}

// Bytes is a wire size: 10603 → "10.6kB". Decimal thousands, matching the
// token counts — these are payload sizes being eyeballed for scale, not
// disk blocks.
func Bytes(n int64) string {
	// The magnitude belongs to the unit, not to the number: "10.6 kB",
	// never "10.6k B".
	c := TokensCompact(int(n))
	if last := c[len(c)-1]; last < '0' || last > '9' {
		return c[:len(c)-1] + " " + string(last) + "B"
	}
	return c + " B"
}

// trimUnit renders one decimal and drops a redundant ".0".
func trimUnit(v float64, unit string) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return s + unit
}
