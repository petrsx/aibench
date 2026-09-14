// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/petrsx/aibench/internal/capture"
)

// alwaysRedacted are the standard auth headers whose value never shows,
// whatever the provider. Profile-declared headers (gateway keys) are
// redacted too via captureTransport.Redact.
var alwaysRedacted = map[string]bool{
	"authorization": true,
	"api-key":       true,
	"x-api-key":     true,
}

// captureTransport wraps an http.RoundTripper and reports the complete
// exchange as capture.Events: one KindRequest event when response headers
// arrive, one KindBody event when the (possibly streamed) body has been fully
// read.
//
// It is installed as the innermost transport (see newHTTPClient) so it sees the
// request exactly as sent, auth headers included.
type captureTransport struct {
	Next http.RoundTripper
	Ch   chan<- capture.Event
	// API is the wire this client speaks; every request event is stamped
	// with it, so a record can later be read by its own api's rules.
	API string
	// Redact names extra request headers (lower-case) whose value should
	// be hidden — the profile's custom headers, which may carry secrets.
	Redact map[string]bool
}

func (t *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req, reqBody, reqBytes := captureRequest(req)

	start := time.Now()
	resp, err := t.Next.RoundTrip(req)
	ttfb := time.Since(start).Round(time.Millisecond)

	if err != nil {
		// An interrupt (esc) cancels the request context, so the transport
		// reports it like any other failure. It is the user's own doing,
		// not an endpoint fault: the record says so plainly rather than
		// dressing it up as a dial error the developer would go chasing.
		text := fmt.Sprintf("%s %s failed after %s: %v", req.Method, req.URL.Path, ttfb, err)
		if errors.Is(err, context.Canceled) {
			text = fmt.Sprintf("%s %s interrupted after %s", req.Method, req.URL.Path, ttfb)
		}
		t.emit(capture.Event{Kind: capture.KindError, Time: start, Text: text})
		return resp, err
	}

	e := capture.Event{
		Kind:        capture.KindRequest,
		Time:        start,
		API:         t.API,
		Method:      req.Method,
		Path:        req.URL.Path,
		Query:       req.URL.RawQuery,
		Status:      resp.StatusCode,
		Duration:    ttfb,
		ReqHeaders:  redactedHeaders(req.Header, t.Redact),
		ReqBody:     reqBody,
		ReqBytes:    reqBytes,
		RespHeaders: redactedHeaders(resp.Header, nil),
	}
	t.emit(e)

	if resp.Body != nil {
		resp.Body = &bodyRecorder{rc: resp.Body, t: t, start: start}
	}
	return resp, nil
}

func (t *captureTransport) emit(e capture.Event) {
	// Never block a request on a full diagnostics buffer.
	select {
	case t.Ch <- e:
	default:
	}
}

// captureRequest reads the request body for the event (capped, with the
// full byte size reported alongside) and hands back a clone whose body
// still carries the full payload.
func captureRequest(req *http.Request) (*http.Request, string, int64) {
	if req.Body == nil {
		return req, "", 0
	}
	b, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return req, fmt.Sprintf("(body read error: %v)", err), 0
	}
	clone := req.Clone(req.Context())
	clone.Body = io.NopCloser(bytes.NewReader(b))
	clone.ContentLength = int64(len(b))

	size := int64(len(b))
	if len(b) > capture.MaxBody {
		return clone, string(b[:capture.MaxBody]), size
	}
	// Pretty-print JSON request bodies; keep anything else verbatim.
	var buf bytes.Buffer
	if json.Indent(&buf, b, "", "  ") == nil {
		return clone, buf.String(), size
	}
	return clone, string(b), size
}

// redactedHeaders converts request/response headers to capture.KeyValues,
// hiding the standard auth headers plus any profile-declared secret headers.
func redactedHeaders(h http.Header, extra map[string]bool) []capture.KeyValue {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]capture.KeyValue, 0, len(keys))
	for _, k := range keys {
		v := strings.Join(h[k], ", ")
		lk := strings.ToLower(k)
		if alwaysRedacted[lk] || extra[lk] {
			v = "•••redacted•••"
		}
		out = append(out, capture.KeyValue{Key: k, Value: v})
	}
	return out
}

// bodyRecorder tees the response body and emits a KindBody event once the
// stream has been fully read (or closed early).
type bodyRecorder struct {
	rc    io.ReadCloser
	t     *captureTransport
	start time.Time
	buf   bytes.Buffer
	n     int64
	once  sync.Once
}

func (r *bodyRecorder) Read(p []byte) (int, error) {
	n, err := r.rc.Read(p)
	if n > 0 {
		r.n += int64(n)
		if room := capture.MaxBody - r.buf.Len(); room > 0 {
			r.buf.Write(p[:min(n, room)])
		}
	}
	if err == io.EOF {
		r.finish()
	}
	return n, err
}

func (r *bodyRecorder) Close() error {
	r.finish()
	return r.rc.Close()
}

func (r *bodyRecorder) finish() {
	r.once.Do(func() {
		r.t.emit(capture.Event{
			Kind:      capture.KindBody,
			Time:      time.Now(),
			Duration:  time.Since(r.start).Round(time.Millisecond),
			Body:      r.buf.String(),
			BodyBytes: r.n,
			Truncated: r.n > int64(r.buf.Len()),
		})
	})
}
