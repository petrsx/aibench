// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/petrsx/aibench/internal/config"
)

// TestRunSend drives the one-shot CLI send against a mock chat endpoint:
// the profile's prompt pair (system prompt + frontmatter params) rides
// the request exactly as a TUI send, the streamed reply lands on stdout,
// and the summary line (profile, model, usage) on stderr.
func TestRunSend(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"Hello"}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	promptFile := filepath.Join(t.TempDir(), "prompt.md")
	prompt := "---\ntemperature: \"0.2\"\n---\nBe terse."
	if err := os.WriteFile(promptFile, []byte(prompt), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Kind:       config.KindModel,
		API:        config.APIChat,
		APIBase:    srv.URL,
		APIKey:     "test-key",
		Model:      "test-model",
		PromptFile: promptFile,
	}
	var out, errOut bytes.Buffer
	if err := runSend(t.Context(), cfg, "smoke", "ping", &out, &errOut); err != nil {
		t.Fatalf("runSend() error = %v", err)
	}

	if got := out.String(); got != "Hello\n" {
		t.Errorf("stdout = %q; want the streamed reply", got)
	}
	summary := errOut.String()
	for _, want := range []string{"smoke", "test-model", "in 3 out 5"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q missing %q", summary, want)
		}
	}
	// The prompt pair rode the request: system prompt and frontmatter param.
	for _, want := range []string{"Be terse.", `"temperature":0.2`, `"content":"ping"`} {
		if !strings.Contains(body, want) {
			t.Errorf("request body missing %s", want)
		}
	}
}

// TestRunScript pins the scripted conversation: the profile's starter
// prompts play in order, each turn carries the accumulated history (the
// second request must contain the first exchange), user turns echo as a
// "> " transcript, and per-turn summaries land on stderr.
func TestRunScript(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		turn++
		n := turn
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, `data: {"id":"%d","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"reply %d"}}]}`+"\n\n", n, n)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	dir := t.TempDir()
	startersFile := filepath.Join(dir, "script.starters.md")
	script := "## first\nhello there\n\n## second\nand again\n"
	if err := os.WriteFile(startersFile, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Kind:          config.KindModel,
		API:           config.APIChat,
		Stream:        true,
		APIBase:       srv.URL,
		APIKey:        "test-key",
		Model:         "test-model",
		StartersFiles: []string{startersFile},
	}
	var out, errOut bytes.Buffer
	if err := runScript(t.Context(), cfg, "smoke", "", &out, &errOut); err != nil {
		t.Fatalf("runScript() error = %v", err)
	}

	want := "> hello there\nreply 1\n> and again\nreply 2\n"
	if out.String() != want {
		t.Errorf("transcript = %q; want %q", out.String(), want)
	}
	if len(bodies) != 2 {
		t.Fatalf("requests = %d; want 2", len(bodies))
	}
	// Turn two carries the whole conversation so far.
	for _, part := range []string{"hello there", "reply 1", "and again"} {
		if !strings.Contains(bodies[1], part) {
			t.Errorf("second request missing %q", part)
		}
	}
	if got := strings.Count(errOut.String(), "smoke · test-model"); got != 2 {
		t.Errorf("stderr summaries = %d; want one per turn", got)
	}

	// No starters declared: a named error, not a silent no-op.
	cfg.StartersFiles = nil
	if err := runScript(t.Context(), cfg, "smoke", "", &out, &errOut); err == nil {
		t.Error("runScript without starter prompts = nil error; want refusal")
	}

	// An unknown script name is a named error listing what exists.
	cfg.StartersFiles = []string{startersFile}
	if err := runScript(t.Context(), cfg, "smoke", "nope", &out, &errOut); err == nil || !strings.Contains(err.Error(), "no script named") {
		t.Errorf("runScript(nope) error = %v; want the unknown-script refusal", err)
	}
}

// TestRunSendToolLoop pins that the CLI is the app's send with no screen:
// a bound tool call runs, its result goes back up as a follow-up request,
// and the final reply lands on stdout — one HTTP request per model call,
// exactly as the TUI's tool loop does it.
func TestRunSendToolLoop(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	toolHits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet { // the bound tool's stub
			toolHits++
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"trains":["21:05"]}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		w.Header().Set("Content-Type", "text/event-stream")
		if len(bodies) == 1 { // first model call: a tool call
			fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_stations","arguments":"{\"station_code\":\"HWD\"}"}}]}}]}`+"\n\n")
			fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else { // the follow-up carrying the result: the answer
			fmt.Fprint(w, `data: {"id":"2","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"Next train 21:05."}}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	toolsFile := filepath.Join(t.TempDir(), "rail.tools.json")
	toolsJSON := fmt.Sprintf(`{
  "chat": {"tools": [{"type":"function","function":{"name":"get_stations",
    "parameters":{"type":"object","properties":{"station_code":{"type":"string"}}}}}]},
  "bindings": {"get_stations": {"method": "GET", "url": "%s/stations/{station_code}"}}
}`, srv.URL)
	if err := os.WriteFile(toolsFile, []byte(toolsJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Kind:       config.KindModel,
		API:        config.APIChat,
		Stream:     true,
		APIBase:    srv.URL,
		APIKey:     "test-key",
		Model:      "test-model",
		ToolsFiles: []string{toolsFile},
	}
	var out, errOut bytes.Buffer
	if err := runSend(t.Context(), cfg, "smoke", "next train from HWD?", &out, &errOut); err != nil {
		t.Fatalf("runSend() error = %v", err)
	}

	if got := out.String(); got != "Next train 21:05.\n" {
		t.Errorf("stdout = %q; want only the final reply", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if toolHits != 1 || len(bodies) != 2 {
		t.Fatalf("tool hits = %d, model calls = %d; want 1 and 2", toolHits, len(bodies))
	}
	if !strings.Contains(bodies[1], `"role":"tool"`) || !strings.Contains(bodies[1], "21:05") {
		t.Errorf("follow-up request did not carry the tool result: %s", bodies[1])
	}
	if !strings.Contains(errOut.String(), "tool get_stations:") {
		t.Errorf("stderr %q does not report the tool run", errOut.String())
	}
}
