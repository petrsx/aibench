// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package provider

import (
	"slices"
	"testing"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
)

// TestSpecRequestIDSwitch verifies the request-id metric shows the
// generic provider id only — gateway-specific ids are left
// to the Raw tab.
func TestSpecRequestIDSwitch(t *testing.T) {
	tests := []struct {
		name    string
		api     string
		headers []capture.KeyValue
		want    capture.KeyValue
	}{
		{
			name: "generic id wins even behind a gateway",
			api:  config.APIChat,
			headers: []capture.KeyValue{
				{Key: "X-Gateway-Request-Id", Value: "gw-1"},
				{Key: "X-Request-Id", Value: "oai-1"},
			},
			want: capture.KeyValue{Key: "request-id", Value: "oai-1"},
		},
		{
			name:    "OpenAI direct",
			api:     config.APIChat,
			headers: []capture.KeyValue{{Key: "X-Request-Id", Value: "oai-1"}},
			want:    capture.KeyValue{Key: "request-id", Value: "oai-1"},
		},
		{
			name:    "Anthropic direct",
			api:     config.APIMessages,
			headers: []capture.KeyValue{{Key: "Request-Id", Value: "ant-1"}},
			want:    capture.KeyValue{Key: "request-id", Value: "ant-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := SpecFor(tt.api)
			kv, ok := spec.LogMeta[0].Pick(tt.headers)
			if !ok || kv != tt.want {
				t.Errorf("Pick() = %v,%v; want %v", kv, ok, tt.want)
			}
		})
	}
}

func TestSpecResolveRateLimits(t *testing.T) {
	tests := []struct {
		name    string
		api     string
		headers []capture.KeyValue
		want    []capture.KeyValue
	}{
		{
			name: "chat api",
			api:  config.APIChat,
			headers: []capture.KeyValue{
				{Key: "X-Ratelimit-Remaining-Requests", Value: "2499"},
				{Key: "X-Ratelimit-Consumed-Tokens", Value: "8"},
			},
			want: []capture.KeyValue{
				{Key: "requests left", Value: "2499"},
				{Key: "consumed", Value: "8"},
			},
		},
		{
			name: "messages api",
			api:  config.APIMessages,
			headers: []capture.KeyValue{
				{Key: "Anthropic-Ratelimit-Tokens-Remaining", Value: "9981"},
			},
			want: []capture.KeyValue{{Key: "tokens left", Value: "9981"}},
		},
		{
			name: "anthropic behind a gateway limit policy",
			api:  config.APIMessages,
			headers: []capture.KeyValue{
				{Key: "X-Ratelimit-Remaining-Tokens", Value: "10000"},
			},
			want: []capture.KeyValue{{Key: "tokens left", Value: "10000"}},
		},
		{
			name: "azure 429 back-off: retry-after-ms wins, labelled ms",
			api:  config.APIChat,
			headers: []capture.KeyValue{
				{Key: "Retry-After", Value: "6"},
				{Key: "Retry-After-Ms", Value: "6000"},
			},
			want: []capture.KeyValue{{Key: "retry after (ms)", Value: "6000"}},
		},
		{
			name:    "openai 429 back-off: seconds header, labelled s",
			api:     config.APIChat,
			headers: []capture.KeyValue{{Key: "Retry-After", Value: "20"}},
			want:    []capture.KeyValue{{Key: "retry after (s)", Value: "20"}},
		},
		{
			name:    "anthropic 429 back-off: retry-after in seconds",
			api:     config.APIMessages,
			headers: []capture.KeyValue{{Key: "Retry-After", Value: "42"}},
			want:    []capture.KeyValue{{Key: "retry after (s)", Value: "42"}},
		},
		{
			name:    "no headers, no rows",
			api:     config.APIChat,
			headers: []capture.KeyValue{{Key: "Content-Type", Value: "text/event-stream"}},
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(SpecFor(tt.api).RateLimits, tt.headers)
			if len(got) != len(tt.want) {
				t.Fatalf("Resolve() = %v; want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("Resolve()[%d] = %v; want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestSpecParams pins each api's adjustable request properties: the
// Prompt tab builds its inputs from these lists, so a knob missing here
// is a knob the user cannot see.
func TestSpecParams(t *testing.T) {
	names := func(api string) []string {
		var out []string
		for _, d := range SpecFor(api).Params {
			out = append(out, d.Name)
		}
		return out
	}

	openaiWant := []string{
		"temperature", "top_p", "max_tokens", "reasoning_effort", "verbosity",
		"frequency_penalty", "presence_penalty", "seed", "stop",
	}
	if got := names(config.APIChat); !slices.Equal(got, openaiWant) {
		t.Errorf("chat api params = %v; want %v", got, openaiWant)
	}
	// Both the sampling knobs and the reasoning ones: which pair applies
	// is the model's business, and an unset field is never sent.
	responsesWant := []string{"temperature", "top_p", "max_output_tokens", "reasoning.effort", "text.verbosity"}
	if got := names(config.APIResponses); !slices.Equal(got, responsesWant) {
		t.Errorf("responses api params = %v; want %v", got, responsesWant)
	}
	anthropicWant := []string{"temperature", "top_p", "top_k", "max_tokens", "stop"}
	if got := names(config.APIMessages); !slices.Equal(got, anthropicWant) {
		t.Errorf("anthropic params = %v; want %v", got, anthropicWant)
	}
}

// TestBodyShapePerAPI pins the one thing the Inspector reads off a spec
// to measure a request: where that api keeps the conversation, and which
// top-level fields stand outside it. The three wires agree on none of it
// — chat keeps the system prompt among the messages, responses hoists it
// to instructions, anthropic to system — which is exactly why the names
// live here and not in the view.
func TestBodyShapePerAPI(t *testing.T) {
	for _, tc := range []struct {
		api   string
		items string
		unit  string
		parts []string
	}{
		{config.APIChat, "messages", "msg", []string{"tools", "response_format"}},
		{config.APIResponses, "input", "item", []string{"instructions", "tools", "text"}},
		{config.APIMessages, "messages", "msg", []string{"system", "tools"}},
	} {
		got := SpecFor(tc.api).Body
		if got.Items != tc.items || got.Unit != tc.unit {
			t.Errorf("%s: items/unit = %q/%q; want %q/%q", tc.api, got.Items, got.Unit, tc.items, tc.unit)
		}
		if !slices.Equal(got.Parts, tc.parts) {
			t.Errorf("%s: parts = %v; want %v", tc.api, got.Parts, tc.parts)
		}
	}
}

// TestOnlyDefinedParamsAreSent pins the contract that makes offering
// every knob safe: a param the prompt file does not define is absent from
// the request body — not sent as a zero, not sent as an api default.
// Which knobs a model requires is the endpoint's business (a reasoning
// model refuses temperature); what this app owes is never to put a value
// on the wire that nobody wrote down.
func TestOnlyDefinedParamsAreSent(t *testing.T) {
	// The frontmatter of examples/prompts/weather-assistant.md: two knobs
	// set, everything else absent.
	for _, tc := range []struct {
		api    string
		values map[string]string
		want   Params
	}{
		{
			config.APIResponses,
			map[string]string{"reasoning.effort": "medium", "text.verbosity": "low"},
			Params{ReasoningEffort: "medium", Verbosity: "low"},
		},
		{
			config.APIChat,
			map[string]string{"reasoning_effort": "medium", "verbosity": "low"},
			Params{ReasoningEffort: "medium", Verbosity: "low"},
		},
		{config.APIMessages, map[string]string{"temperature": "0.3"}, Params{}},
	} {
		got := SpecFor(tc.api).ParseParams(tc.values)
		if tc.api == config.APIMessages {
			// Anthropic has no reasoning knobs; temperature is its own.
			if got.Temperature == nil || *got.Temperature != 0.3 {
				t.Errorf("%s: temperature = %v; want 0.3", tc.api, got.Temperature)
			}
			if got.ReasoningEffort != "" || got.Verbosity != "" {
				t.Errorf("%s: read reasoning knobs it does not offer: %+v", tc.api, got)
			}
			continue
		}
		if got.ReasoningEffort != tc.want.ReasoningEffort || got.Verbosity != tc.want.Verbosity {
			t.Errorf("%s: effort/verbosity = %q/%q; want %q/%q",
				tc.api, got.ReasoningEffort, got.Verbosity, tc.want.ReasoningEffort, tc.want.Verbosity)
		}
		// Everything nobody wrote down stays unset, so the clients never
		// put it on the wire.
		if got.Temperature != nil || got.TopP != nil || got.MaxTokens != 0 ||
			got.FrequencyPenalty != nil || got.PresencePenalty != nil ||
			got.Seed != nil || len(got.Stop) != 0 || got.TopK != 0 {
			t.Errorf("%s: undefined params came back set: %+v", tc.api, got)
		}
	}
}
