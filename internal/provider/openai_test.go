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
	"sync"
	"testing"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
)

// gatewayHit records what arrived at the fake gateway.
type gatewayHit struct {
	mu     sync.Mutex
	hits   int
	path   string
	query  string
	header http.Header
	body   map[string]any
}

// newGatewayCfg points at an Azure-style deployment route the new way:
// the {model} template in the base plus an api-version query param — no
// host axis. The key travels as Bearer (the provider's native form); a
// gateway header rides verbatim.
func newGatewayCfg(baseURL string) config.Config {
	return config.Config{
		API:     config.APIChat,
		APIBase: baseURL + "/openai/deployments/{model}",
		APIKey:  "test-key",
		Headers: map[string]string{"X-Gateway-Key": "gw-secret"},
		Query:   map[string]string{"api-version": "2024-06-01"},
		Model:   "gpt-5.4-mini",
	}
}

func drain(ch <-chan Delta) (content string, usage *Usage, err error) {
	for d := range ch {
		content += d.Content
		if d.Usage != nil {
			usage = d.Usage
		}
		if d.Err != nil {
			err = d.Err
		}
	}
	return content, usage, err
}

// TestStreamDeploymentRouteShape pins the gateway contract: the Azure
// deployment route spelled as a {model} template with api-version in
// query, the dotted deployment name verbatim in the path and body, auth
// headers, and the sampling params.
func TestStreamDeploymentRouteShape(t *testing.T) {
	var got gatewayHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.mu.Lock()
		got.hits++
		got.path = r.URL.Path
		got.query = r.URL.RawQuery
		got.header = r.Header.Clone()
		_ = json.Unmarshal(body, &got.body)
		got.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"hi"}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newGatewayCfg(srv.URL), make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}

	temp, fpen, ppen := float32(0.5), float32(0.25), float32(-0.5)
	seed := 7
	content, usage, streamErr := drain(c.Stream(t.Context(),
		[]Message{
			{Role: RoleSystem, Content: "be brief"},
			{Role: RoleUser, Content: "hello"},
		},
		Params{
			Temperature: &temp, MaxTokens: 42,
			FrequencyPenalty: &fpen, PresencePenalty: &ppen,
			Seed: &seed, Stop: []string{"END", "STOP"},
		},
		nil,
	))
	if streamErr != nil {
		t.Fatalf("stream error = %v; want nil", streamErr)
	}

	got.mu.Lock()
	defer got.mu.Unlock()

	if want := "/openai/deployments/gpt-5.4-mini/chat/completions"; got.path != want {
		t.Errorf("path = %q; want %q", got.path, want)
	}
	if want := "api-version=2024-06-01"; got.query != want {
		t.Errorf("query = %q; want %q", got.query, want)
	}
	if v := got.header.Get("Authorization"); v != "Bearer test-key" {
		t.Errorf("Authorization header = %q; want %q (a key travels in the provider's native form)", v, "Bearer test-key")
	}
	if v := got.header.Get("X-Gateway-Key"); v != "gw-secret" {
		t.Errorf("custom profile header = %q; want the profile's header sent verbatim", v)
	}

	if v := got.body["model"]; v != "gpt-5.4-mini" {
		t.Errorf("body model = %v; want gpt-5.4-mini (deployment name verbatim)", v)
	}
	if v := got.body["temperature"]; v != 0.5 {
		t.Errorf("body temperature = %v; want 0.5", v)
	}
	if v := got.body["max_tokens"]; v != float64(42) {
		t.Errorf("body max_tokens = %v; want 42", v)
	}
	if _, set := got.body["top_p"]; set {
		t.Errorf("body top_p = %v; want omitted when unset", got.body["top_p"])
	}
	if v := got.body["frequency_penalty"]; v != 0.25 {
		t.Errorf("body frequency_penalty = %v; want 0.25", v)
	}
	if v := got.body["presence_penalty"]; v != -0.5 {
		t.Errorf("body presence_penalty = %v; want -0.5", v)
	}
	if v := got.body["seed"]; v != float64(7) {
		t.Errorf("body seed = %v; want 7", v)
	}
	if stop, _ := got.body["stop"].([]any); len(stop) != 2 || stop[0] != "END" {
		t.Errorf("body stop = %v; want [END STOP]", got.body["stop"])
	}
	if v := got.body["stream"]; v != true {
		t.Errorf("body stream = %v; want true", v)
	}
	msgs, _ := got.body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("body messages len = %d; want 2", len(msgs))
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "be brief" {
		t.Errorf("first message = %v; want system/be brief", first)
	}

	if content != "hi" {
		t.Errorf("streamed content = %q; want %q", content, "hi")
	}
	if usage == nil || *usage != (Usage{Prompt: 1, Completion: 2, Total: 3}) {
		t.Errorf("usage = %+v; want {Prompt:1 Completion:2 Total:3}", usage)
	}
}

// TestExplainContentFilter runs a real request into an Azure-style
// content-filter 400 and verifies Explain condenses it to one line with
// the categories that fired — no raw JSON dump.
func TestExplainContentFilter(t *testing.T) {
	const body = `{"error":{"message":"The response was filtered due to the prompt triggering Azure OpenAI's content management policy. Please modify your prompt and retry.","type":null,"param":"prompt","code":"content_filter","status":400,"innererror":{"code":"ResponsibleAIPolicyViolation","content_filter_result":{"hate":{"filtered":true,"severity":"medium"},"jailbreak":{"detected":false,"filtered":false},"self_harm":{"filtered":false,"severity":"safe"},"sexual":{"filtered":false,"severity":"safe"},"violence":{"filtered":false,"severity":"safe"}}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newGatewayCfg(srv.URL), make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}
	_, _, streamErr := drain(c.Stream(t.Context(),
		[]Message{{Role: RoleUser, Content: "hello"}}, Params{}, nil))
	if streamErr == nil {
		t.Fatal("stream error = nil; want the content-filter error")
	}

	got := Explain(streamErr)
	for _, want := range []string{"400 content_filter", "hate: medium"} {
		if !strings.Contains(got, want) {
			t.Errorf("Explain() = %q; want containing %q", got, want)
		}
	}
	for _, reject := range []string{"innererror", "{", "self_harm", "Please modify"} {
		if strings.Contains(got, reject) {
			t.Errorf("Explain() = %q; want without %q", got, reject)
		}
	}
}

// TestExplainGatewayForbidden covers gateway error shapes the SDK doesn't
// type: a gateway answers 403 with {"statusCode":…,"message":…}, which
// must still yield a readable line rather than a bare status.
func TestExplainGatewayForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{ "statusCode": 403, "message": "Forbidden: the request was blocked by the content safety policy. Contact the gateway team." }`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newGatewayCfg(srv.URL), make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}
	_, _, streamErr := drain(c.Stream(t.Context(),
		[]Message{{Role: RoleUser, Content: "hello"}}, Params{}, nil))
	if streamErr == nil {
		t.Fatal("stream error = nil; want the 403 error")
	}

	got := Explain(streamErr)
	for _, want := range []string{"403", "content safety policy"} {
		if !strings.Contains(got, want) {
			t.Errorf("Explain() = %q; want containing %q", got, want)
		}
	}
	if strings.Contains(got, "Contact the gateway team") {
		t.Errorf("Explain() = %q; want first sentence only", got)
	}
}

// TestStreamNoRetries verifies a failing request is sent exactly
// once: retries would muddy the diagnostics this tool exists to capture.
func TestStreamNoRetries(t *testing.T) {
	var got gatewayHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		got.mu.Lock()
		got.hits++
		got.mu.Unlock()
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newGatewayCfg(srv.URL), make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}

	_, _, streamErr := drain(c.Stream(t.Context(),
		[]Message{{Role: RoleUser, Content: "hello"}}, Params{}, nil))
	if streamErr == nil {
		t.Fatal("stream error = nil; want non-nil for 500 response")
	}

	got.mu.Lock()
	defer got.mu.Unlock()
	if got.hits != 1 {
		t.Errorf("gateway hits = %d; want 1 (no retries)", got.hits)
	}
}

// drainCalls consumes a stream keeping only the completed tool calls (they
// ride the Done delta) and any error.
func drainCalls(ch <-chan Delta) (calls []ToolCall, err error) {
	for d := range ch {
		if len(d.ToolCalls) > 0 {
			calls = d.ToolCalls
		}
		if d.Err != nil {
			err = d.Err
		}
	}
	return calls, err
}

// sseChunk is a minimal chat-completions stream chunk line.
func sseChunk(payload string) string {
	return `data: {"id":"1","object":"chat.completion.chunk","created":1,"model":"m",` + payload + "}\n\n"
}

// TestStreamOverlayVerbatim pins the request overlay contract: the openai
// section's fields (tools, tool_choice, response_format, …) reach the wire
// byte-for-byte — key order included, which structured outputs rely on.
func TestStreamOverlayVerbatim(t *testing.T) {
	var raw []byte
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		raw = body
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk(`"choices":[{"index":0,"delta":{"content":"ok"}}]`))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newGatewayCfg(srv.URL), make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}

	const tools = `[{"type":"function","function":{"name":"get_station_timetable","description":"Live trains.","parameters":{"type":"object","properties":{"station_code":{"type":"string"},"num_mins":{"type":"integer"}},"required":["station_code"],"additionalProperties":false}}}]`
	const respFormat = `{"type":"json_schema","json_schema":{"name":"timetable","strict":true,"schema":{"type":"object","properties":{"zebra":{"type":"string"},"apple":{"type":"string"}},"required":["zebra","apple"],"additionalProperties":false}}}`
	overlay := Overlay{
		config.APIChat: json.RawMessage(
			`{"tools":` + tools + `,"tool_choice":"auto","response_format":` + respFormat + `}`),
		config.APIMessages: json.RawMessage(`{"tools":[{"name":"never-sent"}]}`),
	}

	if _, _, streamErr := drain(c.Stream(t.Context(),
		[]Message{{Role: RoleUser, Content: "hello"}}, Params{}, overlay)); streamErr != nil {
		t.Fatalf("stream error = %v; want nil", streamErr)
	}

	mu.Lock()
	defer mu.Unlock()
	body := string(raw)
	// Byte-for-byte: the authored fragments appear verbatim, key order intact
	// ("zebra" before "apple" pins that no re-marshal reordered the schema).
	for _, want := range []string{`"tools":` + tools, `"tool_choice":"auto"`, `"response_format":` + respFormat} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing verbatim overlay fragment %q\nbody: %s", want, body)
		}
	}
	if strings.Contains(body, "never-sent") {
		t.Error("body carries the messages section; want only the active api's overlay")
	}
}

// TestOverlayLegacyKeysAreInert pins the alias deletion: the old
// dialect-named sections ("openai", "anthropic") are no longer read —
// canonical wire keys only, per "aliases get deleted, not kept".
func TestOverlayLegacyKeysAreInert(t *testing.T) {
	legacy := Overlay{
		"openai":    json.RawMessage(`{"tool_choice":"auto"}`),
		"anthropic": json.RawMessage(`{"top_k":3}`),
	}
	for _, api := range []string{"chat", "messages"} {
		fields, err := legacy.Fields(api)
		if err != nil || fields != nil {
			t.Errorf("Fields(%s) = %v, %v; want nil, nil (legacy sections inert)", api, fields, err)
		}
	}
}

func TestStreamReplaysToolTurns(t *testing.T) {
	var got gatewayHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.mu.Lock()
		_ = json.Unmarshal(body, &got.body)
		got.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk(`"choices":[{"index":0,"delta":{"content":"ok"}}]`))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newGatewayCfg(srv.URL), make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}

	history := []Message{
		{Role: RoleUser, Content: "weather in Paris?"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Name: "get_stations", Arguments: `{"station_code":"HWD"}`}}},
		{Role: RoleTool, ToolCallID: "call_1", Content: `{"trains":[]}`},
	}
	if _, _, streamErr := drain(c.Stream(t.Context(), history, Params{}, nil)); streamErr != nil {
		t.Fatalf("stream error = %v; want nil", streamErr)
	}

	got.mu.Lock()
	defer got.mu.Unlock()
	msgs, _ := got.body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("body messages len = %d; want 3", len(msgs))
	}
	asst, _ := msgs[1].(map[string]any)
	calls, _ := asst["tool_calls"].([]any)
	if asst["role"] != "assistant" || len(calls) != 1 {
		t.Fatalf("assistant message = %v; want one tool_calls entry", asst)
	}
	call, _ := calls[0].(map[string]any)
	fn, _ := call["function"].(map[string]any)
	if call["id"] != "call_1" || call["type"] != "function" ||
		fn["name"] != "get_stations" || fn["arguments"] != `{"station_code":"HWD"}` {
		t.Errorf("replayed tool call = %v; want id/type/function verbatim", call)
	}
	tool, _ := msgs[2].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_1" || tool["content"] != `{"trains":[]}` {
		t.Errorf("tool message = %v; want role/tool_call_id/content", tool)
	}
}

// TestStreamExtractsToolCalls pins the read side: streamed tool_calls
// fragments assemble into completed ToolCalls on the Done delta.
func TestStreamExtractsToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk(`"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_stations","arguments":""}}]}}]`))
		fmt.Fprint(w, sseChunk(`"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"station_code\":\"HWD\"}"}}]}}]`))
		fmt.Fprint(w, sseChunk(`"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]`))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newGatewayCfg(srv.URL), make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}
	calls, streamErr := drainCalls(c.Stream(t.Context(),
		[]Message{{Role: RoleUser, Content: "hello"}}, Params{}, nil))
	if streamErr != nil {
		t.Fatalf("stream error = %v; want nil", streamErr)
	}
	if len(calls) != 1 {
		t.Fatalf("ToolCalls = %d; want 1", len(calls))
	}
	want := ToolCall{ID: "call_1", Name: "get_stations", Arguments: `{"station_code":"HWD"}`}
	if calls[0] != want {
		t.Errorf("ToolCalls[0] = %+v; want %+v", calls[0], want)
	}
}

// TestStreamPreGeneratedToken pins the bring-your-own-token path: a
// bearer minted outside the app (az account get-access-token) rides as
// AZURE_TOKEN, as the api key, or as an explicitly declared
// Authorization header — which wins when both are set. No Entra
// credential is involved, so the app mints nothing.
func TestStreamPreGeneratedToken(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  func(base string) config.Config
		want string
	}{
		{
			name: "as the api key",
			cfg: func(base string) config.Config {
				return config.Config{
					API: config.APIChat, APIBase: base, Model: "m",
					APIKey: "eyJ0-token",
				}
			},
			want: "Bearer eyJ0-token",
		},
		{
			name: "as AZURE_TOKEN, no headers line",
			cfg: func(base string) config.Config {
				return config.Config{
					API: config.APIChat, APIBase: base, Model: "m",
					Token: "eyJ0-token", // the Bearer prefix is the app's
				}
			},
			want: "Bearer eyJ0-token",
		},
		{
			name: "as a declared header",
			cfg: func(base string) config.Config {
				return config.Config{
					API: config.APIChat, APIBase: base, Model: "m",
					Headers: map[string]string{"Authorization": "Bearer eyJ0-token"},
				}
			},
			want: "Bearer eyJ0-token",
		},
		{
			name: "declared header beats the api key",
			cfg: func(base string) config.Config {
				return config.Config{
					API: config.APIChat, APIBase: base, Model: "m",
					APIKey:  "sk-ignored",
					Headers: map[string]string{"Authorization": "Bearer eyJ0-token"},
				}
			},
			want: "Bearer eyJ0-token",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got gatewayHit
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got.mu.Lock()
				got.header = r.Header.Clone()
				got.mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(srv.Close)

			c, err := NewClient(tc.cfg(srv.URL), make(chan capture.Event, 64))
			if err != nil {
				t.Fatalf("NewClient() error = %v; want nil", err)
			}
			if _, _, err := drain(c.Stream(t.Context(), []Message{{Role: RoleUser, Content: "hi"}}, Params{}, nil)); err != nil {
				t.Fatalf("stream error = %v; want nil", err)
			}

			got.mu.Lock()
			defer got.mu.Unlock()
			if v := got.header.Get("Authorization"); v != tc.want {
				t.Errorf("Authorization = %q; want %q", v, tc.want)
			}
		})
	}
}

// TestChatSendsOnlyDefinedParams pins the wire half of the contract for
// the chat api: a prompt file that sets only the reasoning knobs produces
// a body carrying only those. Every other knob the spec offers is absent
// — not a zero, not a default — because which ones a model accepts is the
// endpoint's business and putting an unasked-for value on the wire would
// make that decision for it.
func TestChatSendsOnlyDefinedParams(t *testing.T) {
	var got gatewayHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.mu.Lock()
		_ = json.Unmarshal(body, &got.body)
		got.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk(`"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]`))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newGatewayCfg(srv.URL), make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}

	// The shipped weather prompt's frontmatter, in the chat api's
	// spelling: two knobs, nothing else.
	p := SpecFor(config.APIChat).ParseParams(map[string]string{
		"reasoning_effort": "medium",
		"verbosity":        "low",
	})
	if _, _, err := drain(c.Stream(t.Context(),
		[]Message{{Role: RoleUser, Content: "hello"}}, p, nil)); err != nil {
		t.Fatalf("stream error = %v; want nil", err)
	}

	got.mu.Lock()
	defer got.mu.Unlock()
	if got.body["reasoning_effort"] != "medium" {
		t.Errorf("reasoning_effort = %v; want medium", got.body["reasoning_effort"])
	}
	if got.body["verbosity"] != "low" {
		t.Errorf("verbosity = %v; want low", got.body["verbosity"])
	}
	for _, key := range []string{
		"temperature", "top_p", "max_tokens", "max_completion_tokens",
		"frequency_penalty", "presence_penalty", "seed", "stop",
	} {
		if v, sent := got.body[key]; sent {
			t.Errorf("%s = %v was sent though the prompt never set it", key, v)
		}
	}
}
