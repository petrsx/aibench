// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
)

func newResponsesCfg(baseURL string) config.Config {
	return config.Config{
		API:     config.APIResponses,
		Stream:  true, // the model-kind default Resolve produces
		APIBase: baseURL,
		APIKey:  "test-key",
		Headers: map[string]string{"X-Gateway-Key": "gw-secret"},
		Model:   "gpt-5.4-mini",
	}
}

// respondResponses streams a minimal Responses API reply: one text delta,
// then the completed event carrying the full response (usage included).
func respondResponses(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, `data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"logprobs":[],"delta":"hi","sequence_number":1}`+"\n\n")
	fmt.Fprint(w, `data: {"type":"response.completed","sequence_number":2,"response":{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"m","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi","annotations":[]}]}],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":3}}}`+"\n\n")
}

// TestResponsesStreamV1SurfaceShape pins the Azure v1-surface contract,
// spelled the profile's way — the /openai/v1 suffix written in the base:
// the /openai/v1/responses route with NO api-version query, Bearer auth,
// the deployment name as model, instructions from the system turn,
// store:false, and only the params this api speaks (foreign knobs like
// seed/penalties/stop must never leak into the body).
func TestResponsesStreamV1SurfaceShape(t *testing.T) {
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
		respondResponses(w)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newResponsesCfg(srv.URL+"/openai/v1"), make(chan capture.Event, 64))
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
		Params{
			Temperature: &temp, MaxTokens: 42,
			FrequencyPenalty: &fpen, Seed: &seed, Stop: []string{"END"},
		},
		nil,
	))
	if streamErr != nil {
		t.Fatalf("stream error = %v; want nil", streamErr)
	}

	got.mu.Lock()
	defer got.mu.Unlock()

	if want := "/openai/v1/responses"; got.path != want {
		t.Errorf("path = %q; want %q", got.path, want)
	}
	if got.query != "" {
		t.Errorf("query = %q; want empty (the v1 surface versions implicitly)", got.query)
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
	if v := got.body["instructions"]; v != "be brief" {
		t.Errorf("body instructions = %v; want the system turn", v)
	}
	if v := got.body["temperature"]; v != 0.5 {
		t.Errorf("body temperature = %v; want 0.5", v)
	}
	if v := got.body["max_output_tokens"]; v != float64(42) {
		t.Errorf("body max_output_tokens = %v; want 42", v)
	}
	if v := got.body["store"]; v != false {
		t.Errorf("body store = %v; want false (stateless by design)", v)
	}
	if v := got.body["stream"]; v != true {
		t.Errorf("body stream = %v; want true", v)
	}
	for _, foreign := range []string{"seed", "frequency_penalty", "stop", "max_tokens", "messages"} {
		if v, set := got.body[foreign]; set {
			t.Errorf("body %s = %v; want omitted — foreign to the responses api", foreign, v)
		}
	}
	input, _ := got.body["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("body input len = %d; want 1 (system rides instructions)", len(input))
	}
	first, _ := input[0].(map[string]any)
	if first["role"] != "user" || first["content"] != "hello" {
		t.Errorf("first input item = %v; want user/hello", first)
	}

	if content != "hi" {
		t.Errorf("streamed content = %q; want %q", content, "hi")
	}
	if usage == nil || *usage != (Usage{Prompt: 1, Completion: 2, Total: 3}) {
		t.Errorf("usage = %+v; want {Prompt:1 Completion:2 Total:3}", usage)
	}
}

// TestResponsesStreamDirectRoute pins the direct host: the SDK's native
// /responses route on the configured base URL and Bearer auth.
func TestResponsesStreamDirectRoute(t *testing.T) {
	var got gatewayHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.mu.Lock()
		got.path = r.URL.Path
		got.header = r.Header.Clone()
		got.mu.Unlock()
		respondResponses(w)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newResponsesCfg(srv.URL), make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}
	if _, _, streamErr := drain(c.Stream(t.Context(),
		[]Message{{Role: RoleUser, Content: "hello"}}, Params{}, nil)); streamErr != nil {
		t.Fatalf("stream error = %v; want nil", streamErr)
	}

	got.mu.Lock()
	defer got.mu.Unlock()
	if want := "/responses"; got.path != want {
		t.Errorf("path = %q; want %q", got.path, want)
	}
	if v := got.header.Get("Authorization"); v != "Bearer test-key" {
		t.Errorf("Authorization header = %q; want %q", v, "Bearer test-key")
	}
}

// TestResponsesOverlayVerbatim pins the overlay contract for the responses
// section: fields reach the wire byte-for-byte (the flattened Responses
// tool shape, no nested "function" object), and other apis' sections are
// never sent.
func TestResponsesOverlayVerbatim(t *testing.T) {
	var raw []byte
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		raw = body
		mu.Unlock()
		respondResponses(w)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newResponsesCfg(srv.URL), make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}

	const tools = `[{"type":"function","name":"get_station_timetable","description":"Live trains.","parameters":{"type":"object","properties":{"station_code":{"type":"string"}},"required":["station_code"],"additionalProperties":false},"strict":true}]`
	overlay := Overlay{
		config.APIResponses: json.RawMessage(`{"tools":` + tools + `,"tool_choice":"auto"}`),
		config.APIChat:      json.RawMessage(`{"tools":[{"type":"function","function":{"name":"never-sent"}}]}`),
		config.APIMessages:  json.RawMessage(`{"tools":[{"name":"never-sent"}]}`),
	}

	if _, _, streamErr := drain(c.Stream(t.Context(),
		[]Message{{Role: RoleUser, Content: "hello"}}, Params{}, overlay)); streamErr != nil {
		t.Fatalf("stream error = %v; want nil", streamErr)
	}

	mu.Lock()
	defer mu.Unlock()
	body := string(raw)
	for _, want := range []string{`"tools":` + tools, `"tool_choice":"auto"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing verbatim overlay fragment %q\nbody: %s", want, body)
		}
	}
	if strings.Contains(body, "never-sent") {
		t.Error("body carries another api's section; want only the responses overlay")
	}
}

// TestResponsesReplaysToolTurns pins the follow-up request of a tool
// round: the assistant's calls replay as function_call input items and the
// RoleTool turn becomes a function_call_output answering its call_id.
func TestResponsesReplaysToolTurns(t *testing.T) {
	var got gatewayHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.mu.Lock()
		_ = json.Unmarshal(body, &got.body)
		got.mu.Unlock()
		respondResponses(w)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newResponsesCfg(srv.URL), make(chan capture.Event, 64))
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
	input, _ := got.body["input"].([]any)
	if len(input) != 3 {
		t.Fatalf("body input len = %d; want 3 (user, function_call, function_call_output)", len(input))
	}
	call, _ := input[1].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call_1" ||
		call["name"] != "get_stations" || call["arguments"] != `{"station_code":"HWD"}` {
		t.Errorf("replayed function_call = %v; want type/call_id/name/arguments verbatim", call)
	}
	result, _ := input[2].(map[string]any)
	if result["type"] != "function_call_output" || result["call_id"] != "call_1" || result["output"] != `{"trains":[]}` {
		t.Errorf("function_call_output = %v; want type/call_id/output", result)
	}
}

// TestResponsesProfileQuery pins profile-declared query params: a Foundry
// agent endpoint on the direct host needs ?api-version and the preview
// header, both sent verbatim on the SDK's /responses route.
func TestResponsesProfileQuery(t *testing.T) {
	var got gatewayHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.mu.Lock()
		got.path = r.URL.Path
		got.query = r.URL.RawQuery
		got.header = r.Header.Clone()
		got.mu.Unlock()
		respondResponses(w)
	}))
	t.Cleanup(srv.Close)

	cfg := newResponsesCfg(srv.URL + "/agents/assistant/endpoint/protocols/openai")
	cfg.Query = map[string]string{"api-version": "v1"}
	cfg.Headers["Foundry-Features"] = "HostedAgents=V1Preview"
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
	if want := "/agents/assistant/endpoint/protocols/openai/responses"; got.path != want {
		t.Errorf("path = %q; want %q", got.path, want)
	}
	if got.query != "api-version=v1" {
		t.Errorf("query = %q; want api-version=v1 from the profile", got.query)
	}
	if v := got.header.Get("Foundry-Features"); v != "HostedAgents=V1Preview" {
		t.Errorf("Foundry-Features header = %q; want the preview flag", v)
	}
}

// TestResponsesStore pins `store: true` (stateful mode): the first request
// stores with full input and no chain; once state passes the previous
// response's id back, the follow-up chains on previous_response_id and
// carries only the turns after the last assistant reply — instructions
// re-sent (not inherited across the chain), earlier turns omitted.
func TestResponsesStore(t *testing.T) {
	var got gatewayHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.mu.Lock()
		got.hits++
		_ = json.Unmarshal(body, &got.body)
		got.mu.Unlock()
		respondResponses(w)
	}))
	t.Cleanup(srv.Close)

	cfg := newResponsesCfg(srv.URL)
	cfg.Store = true
	c, err := NewClient(cfg, make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}

	// First turn: no previous id yet.
	var respID string
	for d := range c.Stream(t.Context(),
		[]Message{
			{Role: RoleSystem, Content: "be brief"},
			{Role: RoleUser, Content: "hello"},
		}, Params{}, nil) {
		if d.Err != nil {
			t.Fatalf("stream error = %v; want nil", d.Err)
		}
		if d.ResponseID != "" {
			respID = d.ResponseID
		}
	}
	if respID != "resp_1" {
		t.Fatalf("ResponseID = %q; want resp_1 from the completed event", respID)
	}
	got.mu.Lock()
	if v := got.body["store"]; v != true {
		t.Errorf("first body store = %v; want true in stateful mode", v)
	}
	if _, set := got.body["previous_response_id"]; set {
		t.Errorf("first body previous_response_id = %v; want omitted on the first turn", got.body["previous_response_id"])
	}
	got.mu.Unlock()

	// Second turn: state hands the id back; only the new user turn goes up.
	history := []Message{
		{Role: RoleSystem, Content: "be brief"},
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi"},
		{Role: RoleUser, Content: "and in French?"},
	}
	if _, _, streamErr := drain(c.Stream(t.Context(), history, Params{PreviousResponseID: respID}, nil)); streamErr != nil {
		t.Fatalf("stream error = %v; want nil", streamErr)
	}

	got.mu.Lock()
	defer got.mu.Unlock()
	if v := got.body["previous_response_id"]; v != "resp_1" {
		t.Errorf("chained body previous_response_id = %v; want resp_1", v)
	}
	if v := got.body["instructions"]; v != "be brief" {
		t.Errorf("chained body instructions = %v; want re-sent (not inherited across the chain)", v)
	}
	input, _ := got.body["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("chained body input len = %d; want 1 (only the new user turn)", len(input))
	}
	item, _ := input[0].(map[string]any)
	if item["role"] != "user" || item["content"] != "and in French?" {
		t.Errorf("chained input item = %v; want the latest user turn only", item)
	}
}

// TestResponsesExtractsToolCalls pins the read side: completed
// function_call output items land as ToolCalls on the Done delta, keyed by
// call_id (what a function_call_output must answer), not the item id.
func TestResponsesExtractsToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"type":"response.completed","sequence_number":1,"response":{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"m","output":[{"id":"fc_9","type":"function_call","status":"completed","call_id":"call_1","name":"get_stations","arguments":"{\"station_code\":\"HWD\"}"}],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":3}}}`+"\n\n")
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newResponsesCfg(srv.URL), make(chan capture.Event, 64))
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

// TestResponsesAgentBuffered pins the hosted-agent (kind: agent) contract
// learned live 2026-07-19: the agent endpoint answers stream:true with an
// empty event stream, so the client must request WITHOUT streaming (no
// stream flag in the body) and emit the complete reply as one content
// delta followed by the standard Done delta (usage, chain id, raw final).
func TestResponsesAgentBuffered(t *testing.T) {
	var got gatewayHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.mu.Lock()
		got.hits++
		got.path = r.URL.Path
		_ = json.Unmarshal(body, &got.body)
		got.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_agent","object":"response","created_at":1,"status":"completed","model":"m","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"the 14:25 to Heuston","annotations":[]}]}],"usage":{"input_tokens":864,"input_tokens_details":{"cached_tokens":0},"output_tokens":27,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":891}}`)
	}))
	t.Cleanup(srv.Close)

	cfg := newResponsesCfg(srv.URL + "/agents/assistant")
	cfg.Kind = config.KindAgent
	cfg.Store = true
	cfg.Stream = false // the agent-kind default Resolve produces
	c, err := NewClient(cfg, make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}

	var content string
	var done Delta
	for d := range c.Stream(t.Context(), []Message{{Role: RoleUser, Content: "next train?"}}, Params{}, nil) {
		if d.Err != nil {
			t.Fatalf("delta error = %v; want nil", d.Err)
		}
		content += d.Content
		if d.Done {
			done = d
		}
	}

	if got.path != "/agents/assistant/responses" {
		t.Errorf("path = %q; want the agent base + /responses", got.path)
	}
	if _, ok := got.body["stream"]; ok {
		t.Errorf("body carries stream=%v; the agent endpoint must be called without streaming", got.body["stream"])
	}
	if content != "the 14:25 to Heuston" {
		t.Errorf("content = %q; want the full reply as one delta", content)
	}
	if done.ResponseID != "resp_agent" {
		t.Errorf("ResponseID = %q; want the chain id (agent implies store)", done.ResponseID)
	}
	if done.Usage == nil || done.Usage.Prompt != 864 || done.Usage.Completion != 27 {
		t.Errorf("usage = %+v; want 864/27", done.Usage)
	}
	if done.Final == "" {
		t.Error("Final empty; want the raw response body for the Inspector")
	}
}

// TestResponsesReasoningKnobs pins the reasoning-model params: effort
// rides under "reasoning" and verbosity under "text", where the api keeps
// them, and neither field appears at all when it was not set — an unsent
// field leaves the model's own default standing, which is not the same as
// sending a default of ours.
func TestResponsesReasoningKnobs(t *testing.T) {
	var got gatewayHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.mu.Lock()
		_ = json.Unmarshal(body, &got.body)
		got.mu.Unlock()
		respondResponses(w)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(newResponsesCfg(srv.URL+"/openai/v1"), make(chan capture.Event, 64))
	if err != nil {
		t.Fatalf("NewClient() error = %v; want nil", err)
	}

	// The spec offers both sets: which apply is the model's business, not
	// the api's, and an empty field is never sent.
	var names []string
	for _, p := range SpecFor(config.APIResponses).Params {
		names = append(names, p.Name)
	}
	for _, want := range []string{"reasoning.effort", "text.verbosity"} {
		if !slices.Contains(names, want) {
			t.Errorf("the responses spec offers no %q param: %v", want, names)
		}
	}

	// Spelled the way the prompt file spells them, through the same
	// parser the Prompt tab uses.
	p := SpecFor(config.APIResponses).ParseParams(map[string]string{
		"reasoning.effort": "high",
		"text.verbosity":   "low",
	})
	if _, _, err := drain(c.Stream(t.Context(),
		[]Message{{Role: RoleUser, Content: "hello"}}, p, nil)); err != nil {
		t.Fatalf("stream error = %v; want nil", err)
	}

	got.mu.Lock()
	defer got.mu.Unlock()
	reasoning, ok := got.body["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("body has no reasoning object: %v", got.body)
	}
	if reasoning["effort"] != "high" {
		t.Errorf("reasoning.effort = %v; want high", reasoning["effort"])
	}
	text, ok := got.body["text"].(map[string]any)
	if !ok {
		t.Fatalf("body has no text object: %v", got.body)
	}
	if text["verbosity"] != "low" {
		t.Errorf("text.verbosity = %v; want low", text["verbosity"])
	}
	if _, sent := got.body["temperature"]; sent {
		t.Error("temperature was sent though it was never set")
	}
}
