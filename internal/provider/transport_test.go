// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
)

type stubRoundTripper struct {
	resp *http.Response
	err  error
}

func (s stubRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return s.resp, s.err
}

func transportRequest(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://gw.example.net/openai/deployments/m/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestTransportEmitsRequestEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("x-ratelimit-remaining-tokens", "9981")
	rec.Header().Set("x-ratelimit-remaining-requests", "2499")
	rec.Header().Set("x-unrelated", "ignored")
	rec.WriteHeader(http.StatusTooManyRequests)

	ch := make(chan capture.Event, 1)
	tr := &captureTransport{Next: stubRoundTripper{resp: rec.Result()}, Ch: ch}

	if _, err := tr.RoundTrip(transportRequest(t)); err != nil {
		t.Fatalf("RoundTrip() error = %v; want nil", err)
	}

	e := <-ch
	if e.Kind != capture.KindRequest {
		t.Fatalf("Kind = %v; want KindRequest", e.Kind)
	}
	if e.Method != http.MethodPost || e.Status != http.StatusTooManyRequests {
		t.Errorf("event = %s %d; want POST 429", e.Method, e.Status)
	}
	if !strings.Contains(e.Path, "/chat/completions") {
		t.Errorf("Path = %q; want the request path", e.Path)
	}
	// Headers are captured verbatim; interpreting them is provider.Spec's job.
	found := false
	for _, kv := range e.RespHeaders {
		if strings.EqualFold(kv.Key, "x-ratelimit-remaining-tokens") && kv.Value == "9981" {
			found = true
		}
	}
	if !found {
		t.Errorf("RespHeaders = %v; want x-ratelimit-remaining-tokens captured", e.RespHeaders)
	}
}

func TestTransportEmitsErrorEvent(t *testing.T) {
	ch := make(chan capture.Event, 1)
	tr := &captureTransport{Next: stubRoundTripper{err: errors.New("dial refused")}, Ch: ch}

	if _, err := tr.RoundTrip(transportRequest(t)); err == nil {
		t.Fatal("RoundTrip() error = nil; want the transport error passed through")
	}

	e := <-ch
	if e.Kind != capture.KindError {
		t.Fatalf("Kind = %v; want KindError", e.Kind)
	}
	if !strings.Contains(e.Text, "dial refused") {
		t.Errorf("Text = %q; want containing the cause", e.Text)
	}
}

// An interrupt (esc) cancels the request context, which reaches the
// transport as an ordinary error. The record must name it for what it is,
// not as an endpoint fault the developer would go chasing.
func TestTransportNamesInterrupt(t *testing.T) {
	ch := make(chan capture.Event, 1)
	canceled := fmt.Errorf("Post %q: %w", "https://gw/v1/responses", context.Canceled)
	tr := &captureTransport{Next: stubRoundTripper{err: canceled}, Ch: ch}

	if _, err := tr.RoundTrip(transportRequest(t)); err == nil {
		t.Fatal("RoundTrip() error = nil; want the canceled error passed through")
	}

	e := <-ch
	if !strings.Contains(e.Text, "interrupted") {
		t.Errorf("Text = %q; want it to say the request was interrupted", e.Text)
	}
	if strings.Contains(e.Text, "failed") {
		t.Errorf("Text = %q; an interrupt must not read as a failure", e.Text)
	}
}

// A full (or reader-less) channel must never block a request.
func TestTransportDropsWhenChannelFull(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.WriteHeader(http.StatusOK)

	ch := make(chan capture.Event) // unbuffered, nobody reading
	tr := &captureTransport{Next: stubRoundTripper{resp: rec.Result()}, Ch: ch}

	// Deadlocks here (caught by the test timeout) mean the guarantee broke.
	if _, err := tr.RoundTrip(transportRequest(t)); err != nil {
		t.Fatalf("RoundTrip() error = %v; want nil", err)
	}
}

// echoRoundTripper asserts the outgoing body survived capture and answers
// with a fixed response body.
type echoRoundTripper struct {
	t        *testing.T
	wantBody string
	respBody string
}

func (s echoRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var got []byte
	if req.Body != nil {
		var err error
		if got, err = io.ReadAll(req.Body); err != nil {
			s.t.Fatal(err)
		}
	}
	if string(got) != s.wantBody {
		s.t.Errorf("body as sent = %q; want %q", got, s.wantBody)
	}
	rec := httptest.NewRecorder()
	if _, err := rec.WriteString(s.respBody); err != nil {
		s.t.Fatal(err)
	}
	return rec.Result(), nil
}

func TestTransportCapturesFullExchange(t *testing.T) {
	ch := make(chan capture.Event, 2)
	reqBody := `{"model":"gpt-5.4-mini"}`
	tr := &captureTransport{
		Next: echoRoundTripper{t: t, wantBody: reqBody, respBody: "data: chunk\n\n"},
		Ch:   ch,
	}

	req, err := http.NewRequest(http.MethodPost,
		"https://gw.example.net/openai/deployments/m/chat/completions?api-version=2024-06-01",
		strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer super-secret")
	req.Header.Set("Content-Type", "application/json")

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}

	e := <-ch
	if e.Kind != capture.KindRequest {
		t.Fatalf("first event Kind = %v; want KindRequest", e.Kind)
	}
	if e.Query != "api-version=2024-06-01" {
		t.Errorf("Query = %q; want the api-version param", e.Query)
	}
	if !strings.Contains(e.ReqBody, "gpt-5.4-mini") {
		t.Errorf("ReqBody = %q; want captured request body", e.ReqBody)
	}
	if e.ReqBytes != int64(len(reqBody)) {
		t.Errorf("ReqBytes = %d; want %d (the raw size as sent)", e.ReqBytes, len(reqBody))
	}
	for _, kv := range e.ReqHeaders {
		if kv.Key == "Authorization" && strings.Contains(kv.Value, "super-secret") {
			t.Error("Authorization header value was not redacted")
		}
	}

	// Drain the response like the SSE reader would, then expect KindBody.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	be := <-ch
	if be.Kind != capture.KindBody {
		t.Fatalf("second event Kind = %v; want KindBody", be.Kind)
	}
	if be.Body != string(body) || be.BodyBytes != int64(len(body)) {
		t.Errorf("recorded body = %q (%dB); want %q (%dB)", be.Body, be.BodyBytes, body, len(body))
	}
	if be.Truncated {
		t.Error("Truncated = true for a small body; want false")
	}
}

func TestBodyRecorderCapsCapture(t *testing.T) {
	ch := make(chan capture.Event, 2)
	big := strings.Repeat("x", capture.MaxBody+100)
	tr := &captureTransport{Next: echoRoundTripper{t: t, wantBody: "", respBody: big}, Ch: ch}

	resp, err := tr.RoundTrip(transportRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	<-ch // KindRequest
	be := <-ch
	if len(be.Body) != capture.MaxBody || !be.Truncated || be.BodyBytes != int64(len(big)) {
		t.Errorf("capped capture = %dB truncated=%v total=%d; want %d/true/%d",
			len(be.Body), be.Truncated, be.BodyBytes, capture.MaxBody, len(big))
	}
}

// TestTransportCapsRequestCapture pins the request-side display cap: a body
// over capture.MaxBody is kept as a prefix with the full size reported
// (ReqBytes), while the request goes out to the wire complete.
func TestTransportCapsRequestCapture(t *testing.T) {
	ch := make(chan capture.Event, 2)
	big := strings.Repeat("y", capture.MaxBody+100)
	tr := &captureTransport{Next: echoRoundTripper{t: t, wantBody: big, respBody: "ok"}, Ch: ch}

	req, err := http.NewRequest(http.MethodPost, "https://gw.example.net/x", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	resp.Body.Close()

	e := <-ch
	if len(e.ReqBody) != capture.MaxBody || e.ReqBytes != int64(len(big)) {
		t.Errorf("request capture = %dB with ReqBytes %d; want %d kept of %d sent",
			len(e.ReqBody), e.ReqBytes, capture.MaxBody, len(big))
	}
}

// TestCaptureStampsTheAPI pins that a record can say which wire produced
// it: the Inspector reads an old body by the rules of the api that
// actually sent it, not by whichever profile is active when it is read.
func TestCaptureStampsTheAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	ch := make(chan capture.Event, 8)
	client := newHTTPClient(config.APIResponses, ch, nil)
	resp, err := client.Post(srv.URL, "application/json", strings.NewReader(`{"input":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	for {
		select {
		case e := <-ch:
			if e.Kind != capture.KindRequest {
				continue
			}
			if e.API != config.APIResponses {
				t.Fatalf("request event API = %q; want %q", e.API, config.APIResponses)
			}
			return
		default:
			t.Fatal("no request event was emitted")
		}
	}
}
