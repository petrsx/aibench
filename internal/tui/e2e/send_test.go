// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package e2e

// Send flows against httptest mocks: the streamed request lifecycle,
// error records, and the bound-tool loop.

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/petrsx/aibench/internal/pricing"
	"github.com/petrsx/aibench/internal/tui"

	tea "charm.land/bubbletea/v2"
)

// sseChat serves a minimal chat-completions stream answering every POST
// with one content chunk.
func sseChat(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":%q}}]}`+"\n\n", reply)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSendErrorRecord pins the failed-send rendering: a 403 from the
// gateway leaves the record line (path + status) under the message with
// the error bubble, and the Inspector opens on that record's dump.
func TestSendErrorRecord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{ "statusCode": 403, "message": "Forbidden: blocked by the content safety policy." }`)
	}))
	t.Cleanup(srv.Close)

	a := newApp(t, cfgFor(srv.URL), 100, 30)
	a.typeText("hi")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("chat/completions", "content safety policy")

	// The Inspector opens on the request sub-view — what went up is what a
	// prompt-testing session came to read — so the failure body is one tab
	// away, on the response side.
	a.send(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if v := a.plain(); !strings.Contains(v, "403") {
		t.Error("inspector request view missing the status")
	}
	a.send(tea.KeyPressMsg{Code: tea.KeyTab})
	v := a.plain()
	for _, want := range []string{"403", "content safety policy"} {
		if !strings.Contains(v, want) {
			t.Errorf("inspector response view missing %q", want)
		}
	}
}

// TestToolLoop pins the bound-tool round trip: the mock model requests a
// tool call, aibench runs the binding against the stub, feeds the result
// back, and the follow-up answer lands in the transcript.
func TestToolLoop(t *testing.T) {
	var posts, toolHits atomic.Int32
	var firstBody atomic.Value // the first model call's request body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet { // the bound tool's stub
			toolHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"trains":["21:05"]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		body, _ := io.ReadAll(r.Body)
		if posts.Add(1) == 1 { // first model call: a tool call
			firstBody.Store(string(body))
			fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_stations","arguments":"{\"station_code\":\"HWD\"}"}}]}}]}`+"\n\n")
			fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else { // follow-up carrying the result: the answer
			fmt.Fprint(w, `data: {"id":"2","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"Next train 21:05."}}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	promptFile := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(promptFile, []byte("You are a rail assistant.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reqJSON := fmt.Sprintf(`{
  "chat": {
    "tools": [
      { "type": "function",
        "function": {
          "name": "get_stations",
          "description": "Live trains serving a station.",
          "parameters": { "type": "object",
            "properties": { "station_code": { "type": "string" } },
            "required": ["station_code"], "additionalProperties": false } } }
    ],
    "tool_choice": "auto"
  },
  "bindings": {
    "get_stations": { "method": "GET", "url": "%s/stations/{station_code}" }
  }
}`, srv.URL)
	toolsFile := filepath.Join(dir, "rail.tools.json")
	if err := os.WriteFile(toolsFile, []byte(reqJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	// A second, shared tools file — its tool must merge into the request.
	shared := filepath.Join(dir, "shared.tools.json")
	sharedJSON := `{"chat": {"tools": [{"type":"function","function":{"name":"websearch","parameters":{"type":"object"}}}]},
	  "bindings": {"websearch": {"type":"static","result":"nothing found"}}}`
	if err := os.WriteFile(shared, []byte(sharedJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := cfgFor(srv.URL)
	cfg.PromptFile = promptFile
	cfg.ToolsFiles = []string{toolsFile, shared} // declared, merged in order
	a := newApp(t, cfg, 100, 30)
	a.typeText("weather in Paris?")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("get_stations", "Next train 21:05.")

	if toolHits.Load() != 1 {
		t.Errorf("tool stub hits = %d; want exactly 1", toolHits.Load())
	}
	if posts.Load() != 2 {
		t.Errorf("model calls = %d; want the send + the tool round", posts.Load())
	}
	// Both declared files' tools rode the request: the set's own and the
	// shared one, merged into a single chat.tools array.
	body, _ := firstBody.Load().(string)
	if !strings.Contains(body, "get_stations") || !strings.Contains(body, "websearch") {
		t.Errorf("first request missing a merged tool; body = %s", body)
	}
}

// TestSendQueuesWhileStreaming pins the queue: Enter during an in-flight
// send holds the message instead of dropping the keystroke, and it goes up
// on its own turn once the reply completes. Typing while waiting is the
// normal rhythm of a testing session.
func TestSendQueuesWhileStreaming(t *testing.T) {
	release := make(chan struct{})
	var seen []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, string(body))
		first := len(seen) == 1
		mu.Unlock()
		if first {
			<-release // hold the first reply open so the second Enter queues
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"ok"}}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	a := newApp(t, cfgFor(srv.URL), 100, 30)
	a.typeText("first")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitFrame(func(p string) bool { return strings.Contains(p, "first") })

	// Second Enter while the first is still streaming: held, not dropped.
	a.typeText("second")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitFrame(func(p string) bool { return strings.Contains(p, "queued") })
	if !strings.Contains(a.plain(), "second") {
		t.Error("a queued message must be visible in the transcript, not hidden behind a count")
	}
	mu.Lock()
	n := len(seen)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("requests so far = %d; want 1 — the queued message must not go up yet", n)
	}

	// The queued text is on screen already, marked as not-yet-sent, so the
	// dispatch has to be waited on by request count — and from inside
	// waitFrame, which is what advances the model.
	close(release)
	a.waitFrame(func(string) bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) == 2
	})
	mu.Lock()
	n = len(seen)
	mu.Unlock()
	if n != 2 {
		t.Fatalf("requests = %d; want 2 — the queued message never went up", n)
	}
	// Dispatched means no longer queued: the marker clears.
	a.waitFrame(func(p string) bool { return !strings.Contains(p, "queued") })
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(seen[1], "second") {
		t.Errorf("second request body missing the queued text:\n%s", seen[1])
	}
	// The queued turn carries the first exchange as context, not a fresh start.
	if !strings.Contains(seen[1], "first") {
		t.Errorf("second request lost the earlier turns:\n%s", seen[1])
	}
}

// TestPricingSources pins where rates come from and when the app reaches
// for them: the local pricing.yaml is the source, and the published table
// is fetched only when there is no local file — so a fresh install shows
// costs, and a session that can already price itself calls no one.
func TestPricingSources(t *testing.T) {
	published := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("test-model:\n    input: 100\n    output: 100\n    context: 400000\n"))
	}))
	t.Cleanup(published.Close)
	t.Setenv("PRICING_URL", published.URL)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"priced"}}],"usage":{"prompt_tokens":1000,"completion_tokens":1000}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	t.Run("no local file: the published table prices the send", func(t *testing.T) {
		t.Setenv("PRICING_FILE", filepath.Join(t.TempDir(), "absent.yaml"))
		a := newApp(t, cfgFor(srv.URL), 120, 40)
		a.typeText("hi")
		a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
		// 2000 tokens at $100/1M in+out = $0.20.
		a.waitText("priced", "cost", "$0.20")
	})

	t.Run("a local file is the whole answer", func(t *testing.T) {
		local := filepath.Join(t.TempDir(), "pricing.yaml")
		if err := os.WriteFile(local, []byte("test-model:\n    input: 1000\n    output: 1000\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PRICING_FILE", local)
		a := newApp(t, cfgFor(srv.URL), 120, 40, func(m *tui.Model) {
			m.SetPricing(pricing.Runtime()) // as app.Run wires the local file
		})
		a.typeText("hi")
		a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
		// The local $1000/1M, not the published $100/1M: $2.00 — and the
		// published table was never asked for.
		a.waitText("priced", "cost", "$2.00")
	})
}

// TestPricingReload pins pricing.yaml's hot-reload: the watcher's signal
// re-reads the local file and the cost row reprices a send already on
// screen (the fsnotify → channel wiring lives in internal/preset).
func TestPricingReload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"priced"}}],"usage":{"prompt_tokens":1000,"completion_tokens":1000}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	local := filepath.Join(t.TempDir(), "pricing.yaml")
	writeRates := func(perM string) {
		t.Helper()
		if err := os.WriteFile(local, []byte("test-model:\n    input: "+perM+"\n    output: "+perM+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeRates("1000")
	t.Setenv("PRICING_FILE", local)

	pricingCh := make(chan struct{}, 1)
	a := newApp(t, cfgFor(srv.URL), 120, 40, func(m *tui.Model) {
		m.SetPricing(pricing.Runtime())
		m.WatchPricing(pricingCh)
	})
	a.typeText("hi")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("priced", "cost", "$2.00") // 2000 tokens at $1000/1M

	writeRates("2000")
	pricingCh <- struct{}{}
	a.waitText("$4.00") // the same record, repriced from the edited file
}

// TestPricingUpdateCommand pins /pricing-update: the catwalk merge runs
// off the loop, the notice reports what changed, and a record already on
// screen reprices from the refreshed file without a restart.
func TestPricingUpdateCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"priced"}}],"usage":{"prompt_tokens":1000,"completion_tokens":1000}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	catwalk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"models":[{"id":"test-model","cost_per_1m_in":2000,"cost_per_1m_out":2000,"context_window":8192}]}]`))
	}))
	t.Cleanup(catwalk.Close)
	t.Setenv("CATWALK_URL", catwalk.URL)

	local := filepath.Join(t.TempDir(), "pricing.yaml")
	if err := os.WriteFile(local, []byte("test-model:\n    input: 1000\n    output: 1000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PRICING_FILE", local)

	a := newApp(t, cfgFor(srv.URL), 120, 40, func(m *tui.Model) {
		m.SetPricing(pricing.Runtime())
	})
	a.typeText("hi")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("priced", "cost", "$2.00")

	a.typeText("/pricing-update")
	a.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.waitText("1 models (0 added, 1 updated, 0 kept)", "$4.00")
}
