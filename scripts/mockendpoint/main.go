// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Command mockendpoint serves a canned, streaming chat-completions
// response so a recorded demo (scripts/vhs) or a manual smoke run can
// show a real send — streaming text, a record line, usage — without a
// live gateway and without the developer's own credentials.
//
// It is a prop, not a fake API: one route shape, no auth, no error
// modes. Anything that needs real endpoint behaviour belongs in the e2e
// tests (httptest, in-process) or in `make acc-test` (live).
//
//	go run ./scripts/mockendpoint            # 127.0.0.1:8099
//	go run ./scripts/mockendpoint -addr :9000
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// replies answer the question that was actually asked: a recorded demo
// is read by whoever watches it, and a canned reply that ignores the
// prompt above it reads as broken. The triggers cover the shipped
// starter script (examples/prompts/weather-assistant.starters.md); the
// last entry is the catch-all, so any other question still gets an
// answer in the assistant's voice.
var replies = []struct{ match, text string }{
	{"help", "I can check current conditions and short-term forecasts for a place " +
		"you name, and say whether they suit an activity like a walk or a run. " +
		"Ask me about anywhere."},
	{"run", "Good conditions for it after 18:00, once the rain clears — 17 °C and " +
		"light wind. Earlier in the afternoon you would be running through drizzle."},
	{"tomorrow", "Tomorrow looks brighter: 21 °C, broken cloud through the morning " +
		"and clear by mid-afternoon. No rain forecast, winds under 15 km/h."},
	{"", "Paris is 18 °C and overcast right now, with light rain likely after 18:00. " +
		"Winds are light from the south-west at 11 km/h and humidity is around 72%."},
}

// ask is what the request carried: the answer it should get, the model
// it named, and roughly how many tokens went up. The prompt count has to
// grow with the conversation — a demo whose whole subject is measurement
// must not show a context row that never moves.
type ask struct {
	reply  string
	model  string
	prompt int
}

// read picks the answer by the last thing the user said, and sizes the
// prompt the way the app itself estimates one before real usage arrives:
// a quarter of the characters sent.
func read(body []byte) ask {
	var req struct {
		Model    string                           `json:"model"`
		Messages []struct{ Role, Content string } `json:"messages"`
	}
	a := ask{model: "mock", prompt: 1}
	last := ""
	if json.Unmarshal(body, &req) == nil {
		chars := 0
		for _, m := range req.Messages {
			chars += len(m.Content)
			if m.Role == "user" {
				last = strings.ToLower(m.Content)
			}
		}
		a.prompt = max(1, chars/4)
		if req.Model != "" {
			a.model = req.Model
		}
	}
	for _, r := range replies {
		if r.match == "" || strings.Contains(last, r.match) {
			a.reply = r.text
			break
		}
	}
	return a
}

// chunkDelay paces the deltas so the reply visibly streams on screen; a
// burst of text arriving in one frame reads as a buffered response.
const chunkDelay = 28 * time.Millisecond

func main() {
	addr := flag.String("addr", "127.0.0.1:8099", "address to listen on")
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "mockendpoint: POST only", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "mockendpoint: unreadable body", http.StatusBadRequest)
			return
		}
		stream(w, read(body))
	})

	log.Printf("mockendpoint listening on http://%s", *addr)
	srv := &http.Server{Addr: *addr, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

// stream writes the answer as chat-completions SSE: one delta per word,
// then a usage-only chunk (what the real api sends last, and what fills
// the Metrics token rows), then the terminator.
func stream(w http.ResponseWriter, a ask) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "mockendpoint: streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	// A header worth seeing in the Inspector: the real endpoints carry a
	// request id, and the record line picks one up when it is there.
	w.Header().Set("x-request-id", "mock-0f3a91c4")
	w.WriteHeader(http.StatusOK)

	words := strings.Fields(a.reply)
	for i, word := range words {
		if i > 0 {
			word = " " + word
		}
		event(w, map[string]any{
			"id": "chatcmpl-mock", "object": "chat.completion.chunk",
			"created": 0, "model": a.model,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"content": word},
			}},
		})
		flusher.Flush()
		time.Sleep(chunkDelay)
	}

	// Token counts are made up but proportionate, so the graph and the
	// cost rows show plausible shapes rather than zeros.
	completion := len(words) * 4 / 3
	event(w, map[string]any{
		"id": "chatcmpl-mock", "object": "chat.completion.chunk",
		"created": 0, "model": a.model, "choices": []any{},
		"usage": map[string]any{
			"prompt_tokens": a.prompt, "completion_tokens": completion,
			"total_tokens": a.prompt + completion,
		},
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func event(w http.ResponseWriter, payload map[string]any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}
