// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package capture is the captured HTTP request/response data model: the events
// a capturing transport emits (request/response headers, bodies, timings) and
// the store records for the diagnostics views. It is pure data — the transport
// that produces these events lives in internal/provider, and state.Capture
// ingests them into the store.
package capture

import "time"

// MaxBody caps how many bytes of a request or response body are kept.
// Tool-augmented requests (system prompt + tool schemas + history + tool
// results) routinely exceed 8KB, and inspecting the full request is this
// tool's job — so the cap is generous; the store's record bound (500)
// keeps total memory in check.
const MaxBody = 64 << 10

type Kind int

const (
	KindRequest Kind = iota // request sent, response headers received (TTFB)
	KindBody                // response body fully read (total duration)
	KindInfo
	KindError
)

// KeyValue is one metadata item shown on the detail line of a diagnostics
// entry — a captured header, or a labeled metric derived from one.
type KeyValue struct {
	Key, Value string
}

// Event is a single entry in the diagnostics pane. Request events carry
// the structured fields; info/error events just use Text.
type Event struct {
	Kind Kind
	Time time.Time
	Text string

	// API is the wire this request was made on (config.APIChat |
	// APIResponses | APIMessages). A record keeps it because a record
	// outlives the profile that produced it: the Inspector must read an
	// old body by the rules of the api that actually sent it, not by
	// whichever profile happens to be active now.
	API string

	Method   string
	Path     string
	Query    string
	Status   int
	Duration time.Duration // TTFB on KindRequest, total on KindBody
	Meta     []KeyValue

	ReqHeaders  []KeyValue // as actually sent, secrets redacted
	ReqBody     string     // capped at MaxBody
	ReqBytes    int64      // full request-body size, even when ReqBody is cut
	RespHeaders []KeyValue
	Body        string // response body (KindBody only), capped at MaxBody
	BodyBytes   int64  // full size, even when Body is truncated
	Truncated   bool
}

func (e Event) Timestamp() time.Time {
	if e.Time.IsZero() {
		return time.Now()
	}
	return e.Time
}
