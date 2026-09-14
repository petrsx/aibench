// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package provider streams completions from an AI endpoint over a
// transport that captures the raw HTTP traffic. APIs: chat and responses
// (openai-go), messages (anthropic-sdk-go), each on the direct or azure
// host.
//
// Adding an api is one file — its client and its Spec — plus a case in
// NewClient and one in SpecFor. A new *route* or auth scheme usually costs
// no code at all: it is a base-URL spelling, an api.query entry, or an
// api.headers line in the profile.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
)

// Message roles, matching the request-body values across providers.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool" // a tool result fed back to the model
)

// Message is one turn of the conversation.
type Message struct {
	Role    string
	Content string
	// ToolCalls are the calls an assistant turn requested; each client
	// replays them in its own wire shape when the history is re-sent.
	ToolCalls []ToolCall
	// ToolCallID links a RoleTool result back to the call it answers.
	ToolCallID string
}

// ToolCall is one tool invocation the model requested. Arguments is the
// raw JSON argument object, kept verbatim for display and replay.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Overlay carries per-api request-body extensions (tools, tool_choice,
// response_format, …) keyed by api name. Each value is a raw JSON object
// of top-level body fields merged into the request verbatim — the tool
// translates nothing, so what the prompt author wrote is what goes on the
// wire. The Azure host reuses its api's overlay: body extensions are a
// wire-shape concern, not a host one.
type Overlay map[string]json.RawMessage

// Fields decodes the api's overlay into its top-level body fields, values
// kept raw. A missing section is an empty map; the legacy dialect-named
// section ("openai"/"anthropic") is read when the api-named one is absent.
func (o Overlay) Fields(api string) (map[string]json.RawMessage, error) {
	raw, ok := o[api]
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("%s request overlay: %w", api, err)
	}
	return fields, nil
}

// Usage carries the token counts reported by the stream.
type Usage struct {
	Prompt     int
	Completion int
	Total      int
}

// Delta is one chunk of the assistant response; Done marks the end. Final
// rides the Done chunk: the assembled full response body (the SDK's own
// accumulation of the stream), which the Inspector shows in place of the raw
// per-chunk stream — so the verbose chunks need not be kept in memory.
// ToolCalls also ride the Done chunk: the completed calls the model
// requested, extracted from the SDK's accumulation rather than streamed
// argument fragments.
type Delta struct {
	Content   string
	Err       error
	Done      bool
	Usage     *Usage
	Final     string
	ToolCalls []ToolCall
	// ResponseID rides the Done chunk when the endpoint stored the response
	// server-side (the responses api in stored/stateful mode): the id the
	// next request chains on. Empty everywhere else.
	ResponseID string
}

// finalJSON marshals an assembled response object (the SDK's accumulated
// completion) to compact JSON with HTML escaping off, so <, >, and & in the
// content stay literal. It returns "" on error, which the caller treats as
// "no assembled body" and falls back to the raw capture.
func finalJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return ""
	}
	return strings.TrimRight(buf.String(), "\n")
}

// Params are optional sampling parameters; nil/zero fields are left out
// of the request so the backend defaults apply. Providers ignore what
// they don't speak: TopK is Anthropic-only; FrequencyPenalty,
// PresencePenalty, and Seed are chat-api only; Stop maps to
// stop / stop_sequences respectively.
type Params struct {
	Temperature      *float32 // openai 0–2, anthropic 0–1
	TopP             *float32 // 0–1
	TopK             int      // anthropic only
	MaxTokens        int
	FrequencyPenalty *float32 // chat api, −2–2
	PresencePenalty  *float32 // chat api, −2–2
	Seed             *int     // chat api
	Stop             []string

	// The reasoning-model knobs. A reasoning model rejects temperature
	// and top_p and takes these instead: how much thinking to spend, and
	// how much prose to answer with. Empty means the field is not sent at
	// all, so the model's own default stands.
	ReasoningEffort string // none | minimal | low | medium | high | xhigh | max
	Verbosity       string // low | medium | high

	// PreviousResponseID is the responses api's stored-response chain: state
	// sets it from the last stored response's id, and the client then sends
	// only the turns after the last assistant reply. Other apis ignore it.
	PreviousResponseID string
}

// Client streams completions from one endpoint. overlay carries the
// prompt's per-api request-body extensions; each client merges its own
// api's section verbatim.
type Client interface {
	Stream(ctx context.Context, history []Message, p Params, overlay Overlay) <-chan Delta
}

// NewClient picks the SDK from the api; the route is the config's base
// URL template ({model} filled by baseURL) and auth follows the config's
// credential ladder inside each client.
func NewClient(cfg config.Config, captureCh chan<- capture.Event) (Client, error) {
	switch cfg.API {
	case config.APIMessages:
		return newAnthropicClient(cfg, captureCh), nil
	case config.APIChat:
		return newOpenAIClient(cfg, captureCh)
	case config.APIResponses:
		return newResponsesClient(cfg, captureCh)
	default:
		return nil, fmt.Errorf("unknown api %q (want chat, responses, or messages)", cfg.API)
	}
}

// baseURL fills the api.base template before it becomes a client base URL.
// A {model} placeholder is replaced with the configured model — the
// deployment name on Azure's route shape — so a profile can point straight
// at .../openai/deployments/{model} (the SDK then appends the operation
// path). No placeholder means the base is returned unchanged.
func baseURL(cfg config.Config) string {
	return strings.ReplaceAll(cfg.APIBase, "{model}", cfg.Model)
}

// newHTTPClient wraps the exchange transport so every provider records the
// request exactly as sent — auth applied by SDK middleware
// included (redacted for display). Profile-declared header names are
// redacted too, since they may carry gateway secrets.
func newHTTPClient(api string, captureCh chan<- capture.Event, headers map[string]string) *http.Client {
	var redact map[string]bool
	if len(headers) > 0 {
		redact = make(map[string]bool, len(headers))
		for k := range headers {
			redact[strings.ToLower(k)] = true
		}
	}
	return &http.Client{
		Transport: &captureTransport{Next: http.DefaultTransport, Ch: captureCh, API: api, Redact: redact},
		Timeout:   5 * time.Minute,
	}
}
