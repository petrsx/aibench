// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
)

// TestStreamAnthropicRequestShape pins the Messages API contract: the
// /v1/messages route, x-api-key + anthropic-version headers, the system
// prompt as a top-level field, and the required max_tokens default.
func TestStreamAnthropicRequestShape(t *testing.T) {
	var got gatewayHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.mu.Lock()
		got.hits++
		got.path = r.URL.Path
		got.header = r.Header.Clone()
		_ = json.Unmarshal(body, &got.body)
		got.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\n")
		fmt.Fprint(w, `data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":1}}}`+"\n\n")
		fmt.Fprint(w, "event: content_block_start\n")
		fmt.Fprint(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`+"\n\n")
		fmt.Fprint(w, "event: content_block_stop\n")
		fmt.Fprint(w, `data: {"type":"content_block_stop","index":0}`+"\n\n")
		fmt.Fprint(w, "event: message_delta\n")
		fmt.Fprint(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`+"\n\n")
		fmt.Fprint(w, "event: message_stop\n")
		fmt.Fprint(w, `data: {"type":"message_stop"}`+"\n\n")
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		API:     config.APIMessages,
		APIBase: srv.URL,
		APIKey:  "ant-key",
		Headers: map[string]string{"X-Gateway-Key": "gw-secret"},
		Model:   "claude-opus-4-8",
	}
	c, err := NewClient(cfg, make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}

	temp, fpen := float32(0.5), float32(0.25)
	seed := 7
	content, usage, streamErr := drain(c.Stream(t.Context(),
		[]Message{
			{Role: RoleSystem, Content: "be brief"},
			{Role: RoleUser, Content: "hello"},
		},
		// FrequencyPenalty and Seed are foreign to the messages api
		// and must never be sent even when set.
		Params{Temperature: &temp, TopK: 40, Stop: []string{"END"}, FrequencyPenalty: &fpen, Seed: &seed},
		nil,
	))
	if streamErr != nil {
		t.Fatalf("stream error = %v; want nil", streamErr)
	}

	got.mu.Lock()
	defer got.mu.Unlock()

	if want := "/v1/messages"; got.path != want {
		t.Errorf("path = %q; want %q", got.path, want)
	}
	if v := got.header.Get("X-Api-Key"); v != "ant-key" {
		t.Errorf("x-api-key header = %q; want %q", v, "ant-key")
	}
	if v := got.header.Get("Anthropic-Version"); v == "" {
		t.Error("anthropic-version header missing; want set")
	}
	if v := got.header.Get("X-Gateway-Key"); v != "gw-secret" {
		t.Errorf("custom profile header = %q; want the profile's header sent verbatim", v)
	}

	if v := got.body["model"]; v != "claude-opus-4-8" {
		t.Errorf("body model = %v; want claude-opus-4-8", v)
	}
	if v := got.body["max_tokens"]; v != float64(anthropicMaxTokens) {
		t.Errorf("body max_tokens = %v; want default %d", v, anthropicMaxTokens)
	}
	if v := got.body["temperature"]; v != 0.5 {
		t.Errorf("body temperature = %v; want 0.5", v)
	}
	if v := got.body["top_k"]; v != float64(40) {
		t.Errorf("body top_k = %v; want 40", v)
	}
	if stop, _ := got.body["stop_sequences"].([]any); len(stop) != 1 || stop[0] != "END" {
		t.Errorf("body stop_sequences = %v; want [END]", got.body["stop_sequences"])
	}
	for _, foreign := range []string{"frequency_penalty", "seed"} {
		if v, set := got.body[foreign]; set {
			t.Errorf("body %s = %v; want omitted — foreign to the messages api", foreign, v)
		}
	}
	if v := got.body["stream"]; v != true {
		t.Errorf("body stream = %v; want true", v)
	}
	sys, _ := got.body["system"].([]any)
	if len(sys) != 1 {
		t.Fatalf("body system = %v; want one top-level block, not a message turn", got.body["system"])
	}
	msgs, _ := got.body["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("body messages len = %d; want 1 (system excluded)", len(msgs))
	}

	if content != "hi" {
		t.Errorf("streamed content = %q; want %q", content, "hi")
	}
	if usage == nil || *usage != (Usage{Prompt: 5, Completion: 2, Total: 7}) {
		t.Errorf("usage = %+v; want {Prompt:5 Completion:2 Total:7}", usage)
	}
}

// TestStreamFoundryRequestShape pins Anthropic-on-Azure-Foundry: the same
// Messages API (x-api-key + anthropic-version, native SDK), but served
// under the Foundry /anthropic base so the route is /anthropic/v1/messages.
func TestStreamFoundryRequestShape(t *testing.T) {
	var got gatewayHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.mu.Lock()
		got.hits++
		got.path = r.URL.Path
		got.header = r.Header.Clone()
		got.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\n")
		fmt.Fprint(w, `data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":1}}}`+"\n\n")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`+"\n\n")
		fmt.Fprint(w, "event: message_stop\n")
		fmt.Fprint(w, `data: {"type":"message_stop"}`+"\n\n")
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		API: config.APIMessages,
		// The Foundry resource base; the SDK appends /v1/messages.
		APIBase: srv.URL + "/anthropic",
		APIKey:  "foundry-key",
		Model:   "claude-sonnet-4-6",
	}
	c, err := NewClient(cfg, make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}
	if _, _, streamErr := drain(c.Stream(t.Context(),
		[]Message{{Role: RoleUser, Content: "hello"}}, Params{}, nil)); streamErr != nil {
		t.Fatalf("stream error = %v; want nil", streamErr)
	}

	got.mu.Lock()
	defer got.mu.Unlock()
	if want := "/anthropic/v1/messages"; got.path != want {
		t.Errorf("path = %q; want %q (Foundry route)", got.path, want)
	}
	if v := got.header.Get("X-Api-Key"); v != "foundry-key" {
		t.Errorf("x-api-key header = %q; want %q", v, "foundry-key")
	}
	if v := got.header.Get("Anthropic-Version"); v == "" {
		t.Error("anthropic-version header missing; want set")
	}
}

// anthropicToolSSE scripts a reply whose only content is a tool_use block,
// its input streamed as input_json_delta fragments.
func anthropicToolSSE(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "event: message_start\n")
	fmt.Fprint(w, `data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":1}}}`+"\n\n")
	fmt.Fprint(w, "event: content_block_start\n")
	fmt.Fprint(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_stations","input":{}}}`+"\n\n")
	fmt.Fprint(w, "event: content_block_delta\n")
	fmt.Fprint(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"station_code\":\"HWD\"}"}}`+"\n\n")
	fmt.Fprint(w, "event: content_block_stop\n")
	fmt.Fprint(w, `data: {"type":"content_block_stop","index":0}`+"\n\n")
	fmt.Fprint(w, "event: message_delta\n")
	fmt.Fprint(w, `data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":9}}`+"\n\n")
	fmt.Fprint(w, "event: message_stop\n")
	fmt.Fprint(w, `data: {"type":"message_stop"}`+"\n\n")
}

// TestStreamAnthropicToolRound pins the messages api's tool contract in
// one round trip: the anthropic overlay section reaches the wire verbatim,
// replayed tool turns take their wire shape (assistant tool_use block, user
// tool_result block), and a tool_use reply extracts into Delta.ToolCalls.
func TestStreamAnthropicToolRound(t *testing.T) {
	var got gatewayHit
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.mu.Lock()
		raw = body
		_ = json.Unmarshal(body, &got.body)
		got.mu.Unlock()
		anthropicToolSSE(w)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		API:     config.APIMessages,
		APIBase: srv.URL,
		APIKey:  "ant-key",
		Model:   "claude-opus-4-8",
	}
	c, err := NewClient(cfg, make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}

	const tools = `[{"name":"get_stations","description":"Stations.","input_schema":{"type":"object","properties":{"station_code":{"type":"string"}},"required":["station_code"]}}]`
	overlay := Overlay{
		config.APIMessages: json.RawMessage(`{"tools":` + tools + `}`),
		config.APIChat:     json.RawMessage(`{"tools":[{"type":"function"}],"tool_choice":"never-sent"}`),
	}
	history := []Message{
		{Role: RoleUser, Content: "trains?"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "toolu_0", Name: "get_stations", Arguments: `{"station_code":"BFC"}`}}},
		{Role: RoleTool, ToolCallID: "toolu_0", Content: `{"trains":[]}`},
	}
	calls, streamErr := drainCalls(c.Stream(t.Context(), history, Params{}, overlay))
	if streamErr != nil {
		t.Fatalf("stream error = %v; want nil", streamErr)
	}

	got.mu.Lock()
	defer got.mu.Unlock()

	// Overlay: the anthropic section verbatim, the openai one never sent.
	if body := string(raw); !strings.Contains(body, `"tools":`+tools) {
		t.Errorf("body missing verbatim tools fragment\nbody: %s", body)
	} else if strings.Contains(body, "never-sent") {
		t.Error("body carries the openai section; want only the active api's overlay")
	}

	// Replay: assistant tool_use block, then the result as a user tool_result.
	msgs, _ := got.body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("body messages len = %d; want 3", len(msgs))
	}
	asst, _ := msgs[1].(map[string]any)
	blocks, _ := asst["content"].([]any)
	if asst["role"] != "assistant" || len(blocks) != 1 {
		t.Fatalf("assistant message = %v; want one content block", asst)
	}
	tu, _ := blocks[0].(map[string]any)
	input, _ := tu["input"].(map[string]any)
	if tu["type"] != "tool_use" || tu["id"] != "toolu_0" || tu["name"] != "get_stations" || input["station_code"] != "BFC" {
		t.Errorf("tool_use block = %v; want id/name/input replayed", tu)
	}
	res, _ := msgs[2].(map[string]any)
	resBlocks, _ := res["content"].([]any)
	tr, _ := resBlocks[0].(map[string]any)
	if res["role"] != "user" || tr["type"] != "tool_result" || tr["tool_use_id"] != "toolu_0" {
		t.Errorf("tool result message = %v; want a user tool_result block", res)
	}

	// Extraction: the streamed tool_use assembles into the Done ToolCalls.
	if len(calls) != 1 {
		t.Fatalf("ToolCalls = %d; want 1", len(calls))
	}
	want := ToolCall{ID: "toolu_1", Name: "get_stations", Arguments: `{"station_code":"HWD"}`}
	if calls[0] != want {
		t.Errorf("ToolCalls[0] = %+v; want %+v", calls[0], want)
	}
}
