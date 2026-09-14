// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package provider

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/azure"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
)

// openaiSpec presents the OpenAI Chat Completions API on either host.
//
// Documented headers (there is no exhaustive official list; anything not
// named here is observable-but-undocumented infrastructure):
//   - OpenAI rate limits (x-ratelimit-{limit,remaining,reset}-{requests,tokens}):
//     https://developers.openai.com/api/docs/guides/rate-limits
//   - OpenAI x-request-id: the API reference's debugging-requests section
//   - Azure OpenAI rate limits (same x-ratelimit names + retry-after-ms):
//     https://learn.microsoft.com/azure/foundry/openai/how-to/quota#understanding-rate-limits
//   - Gateway ids and token-limit headers vary per gateway and have no
//     fixed names; they show verbatim in the Inspector, not mapped here.
//
// The chat wire keeps the whole conversation in "messages" — the system
// prompt is one of them — so only the tool declarations and the
// structured-output schema stand outside it.
var openaiSpec = Spec{
	// Every entry here is a role — assistant is the longest — and
	// response_format is the longest part beside them.
	Body: BodyShape{
		Items: "messages", Unit: "msg",
		Parts:      []string{"tools", "response_format"},
		LabelWidth: len("response_format"),
	},
	LogMeta: []MetricDef{
		// The generic provider id only; gateway-specific ids stay in
		// the Inspector's raw dump.
		Metric("request-id", "x-request-id"),
		Metric("tokens left", "x-ratelimit-remaining-tokens"),
		Metric("requests left", "x-ratelimit-remaining-requests"),
	},
	RateLimits: []MetricDef{
		// The back-off the endpoint imposes on a 429 — the real "how long
		// to wait", which the HTTP timing (a fast rejection) never shows.
		// Azure OpenAI sends retry-after-ms; the standard header is seconds.
		// Units live in the label since the value passes through verbatim.
		{Candidates: []Candidate{
			{Header: "retry-after-ms", Label: "retry after (ms)"},
			{Header: "retry-after", Label: "retry after (s)"},
		}},
		Metric("requests left", "x-ratelimit-remaining-requests"),
		Metric("requests max", "x-ratelimit-limit-requests"),
		Metric("tokens left", "x-ratelimit-remaining-tokens"),
		Metric("tokens max", "x-ratelimit-limit-tokens"),
		Metric("consumed", "x-ratelimit-consumed-tokens"),
	},
	// The chat api's adjustable request properties:
	// https://platform.openai.com/docs/api-reference/chat/create
	// Every knob the api has, offered whether or not a given model takes
	// it: which ones apply is the model's business (a reasoning model
	// refuses temperature and takes reasoning_effort instead), and what
	// this app owes is the other half — sending only what was filled in.
	Params: []ParamDef{
		{Name: "temperature", Hint: "0–2"},
		{Name: "top_p", Hint: "0–1"},
		{Name: "max_tokens", Hint: ""},
		{Name: "reasoning_effort", Hint: "minimal–max"},
		{Name: "verbosity", Hint: "low/med/high"},
		{Name: "frequency_penalty", Hint: "-2–2"},
		{Name: "presence_penalty", Hint: "-2–2"},
		{Name: "seed", Hint: ""},
		{Name: "stop", Hint: "a,b,…"},
	},
}

// openaiClient serves the chat api via openai-go.
type openaiClient struct {
	api   openai.Client
	model string
}

// newOpenAIClient builds the chat client. The route is entirely the base
// URL's business: baseURL fills a {model} template (e.g. an Azure
// deployment path …/openai/deployments/{model}) and the SDK appends
// /chat/completions; an api-version rides cfg.Query. Auth follows the
// config's ladder — a key travels in the provider's native Bearer form,
// custom header schemes (api-key, subscription keys) ride cfg.Headers
// verbatim, and Entra credentials select token auth by their presence
// (azure.WithTokenCredential is a pure header option, safe with any base).
func newOpenAIClient(cfg config.Config, captureCh chan<- capture.Event) (*openaiClient, error) {
	opts := []option.RequestOption{
		option.WithHTTPClient(newHTTPClient(config.APIChat, captureCh, cfg.Headers)),
		option.WithBaseURL(baseURL(cfg)),
		// One HTTP request per send: retries would muddy the
		// diagnostics this tool exists to capture.
		option.WithMaxRetries(0),
	}

	// Config.validate has already rejected more than one of these, so the
	// order here is presentation, not precedence.
	switch {
	case cfg.APIKey != "":
		opts = append(opts, option.WithAPIKey(cfg.APIKey)) // Bearer token
	case cfg.Token != "":
		// A bearer the user minted themselves: sent as-is, nothing acquired.
		opts = append(opts, option.WithHeader("Authorization", "Bearer "+cfg.Token))
	case cfg.HasServicePrincipal():
		// Entra ID bearer token from the declared app identity.
		cred, err := azidentity.NewClientSecretCredential(cfg.TenantID, cfg.ClientID, cfg.ClientSecret, nil)
		if err != nil {
			return nil, fmt.Errorf("client secret credential: %w", err)
		}
		opts = append(opts, azure.WithTokenCredential(cred,
			azure.WithTokenCredentialScopes([]string{cfg.TokenScope()})))
	case cfg.UseLogin:
		// AZURE_USE_LOGIN: mint from the user's own az / azd sign-in.
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("azure sign-in credential (AZURE_USE_LOGIN): %w", err)
		}
		opts = append(opts, azure.WithTokenCredential(cred,
			azure.WithTokenCredentialScopes([]string{cfg.TokenScope()})))
	}

	for k, v := range cfg.Headers {
		opts = append(opts, option.WithHeader(k, v))
	}
	for k, v := range cfg.Query {
		opts = append(opts, option.WithQueryAdd(k, v))
	}

	return &openaiClient{api: openai.NewClient(opts...), model: cfg.Model}, nil
}

// Stream starts a streaming chat completion and feeds deltas into the
// returned channel until the response is complete.
func (c *openaiClient) Stream(ctx context.Context, history []Message, p Params, overlay Overlay) <-chan Delta {
	out := make(chan Delta, 32)
	req := c.request(history, p)

	// The prompt's request overlay merges into the body verbatim: each raw
	// field rides a WithJSONSet (json.RawMessage marshals byte-for-byte, so
	// key order survives — structured outputs follow the schema order sent).
	fields, err := overlay.Fields(config.APIChat)
	if err != nil {
		go func() { defer close(out); out <- Delta{Err: err, Done: true} }()
		return out
	}
	var opts []option.RequestOption
	for k, v := range fields {
		opts = append(opts, option.WithJSONSet(k, v))
	}

	go c.consume(ctx, req, opts, out)
	return out
}

// request builds the body: the conversation in this api's shape, then the
// params the spec offers on it. Anything the api does not have is simply
// never set — the spec never offers it, and a foreign value is dropped by
// design rather than translated.
func (c *openaiClient) request(history []Message, p Params) openai.ChatCompletionNewParams {
	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(history))
	for _, m := range history {
		switch m.Role {
		case RoleSystem:
			msgs = append(msgs, openai.SystemMessage(m.Content))
		case RoleAssistant:
			msgs = append(msgs, assistantMessage(m))
		case RoleTool:
			msgs = append(msgs, openai.ToolMessage(m.Content, m.ToolCallID))
		default:
			msgs = append(msgs, openai.UserMessage(m.Content))
		}
	}

	req := openai.ChatCompletionNewParams{
		Model:    c.model,
		Messages: msgs,
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}
	if p.Temperature != nil {
		req.Temperature = openai.Float(float64(*p.Temperature))
	}
	if p.TopP != nil {
		req.TopP = openai.Float(float64(*p.TopP))
	}
	if p.MaxTokens > 0 {
		req.MaxTokens = openai.Int(int64(p.MaxTokens))
	}
	if p.FrequencyPenalty != nil {
		req.FrequencyPenalty = openai.Float(float64(*p.FrequencyPenalty))
	}
	if p.PresencePenalty != nil {
		req.PresencePenalty = openai.Float(float64(*p.PresencePenalty))
	}
	if p.ReasoningEffort != "" {
		req.ReasoningEffort = shared.ReasoningEffort(p.ReasoningEffort)
	}
	if p.Verbosity != "" {
		req.Verbosity = openai.ChatCompletionNewParamsVerbosity(p.Verbosity)
	}
	if p.Seed != nil {
		req.Seed = openai.Int(int64(*p.Seed))
	}
	if len(p.Stop) > 0 {
		req.Stop = openai.ChatCompletionNewParamsStopUnion{OfStringArray: p.Stop}
	}

	return req
}

// consume runs the one request and folds the stream into deltas, closing out
// when the response is complete.
func (c *openaiClient) consume(ctx context.Context, req openai.ChatCompletionNewParams, opts []option.RequestOption, out chan<- Delta) {
	defer close(out)

	stream := c.api.Chat.Completions.NewStreaming(ctx, req, opts...)
	defer stream.Close()

	// The accumulator folds the chunks back into a single ChatCompletion so
	// the Inspector can show the final body instead of the raw stream.
	acc := openai.ChatCompletionAccumulator{}
	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)
		var d Delta
		if len(chunk.Choices) > 0 {
			d.Content = chunk.Choices[0].Delta.Content
		}
		if chunk.JSON.Usage.Valid() {
			d.Usage = &Usage{
				Prompt:     int(chunk.Usage.PromptTokens),
				Completion: int(chunk.Usage.CompletionTokens),
				Total:      int(chunk.Usage.TotalTokens),
			}
		}
		if d.Content != "" || d.Usage != nil {
			out <- d
		}
	}
	if err := stream.Err(); err != nil {
		out <- Delta{Err: err, Done: true}
		return
	}
	// Completed tool calls come from the accumulator, not the streamed
	// argument fragments; the state layer decides whether to run them.
	d := Delta{Done: true, Final: finalJSON(acc.ChatCompletion)}
	if len(acc.Choices) > 0 {
		for _, tc := range acc.Choices[0].Message.ToolCalls {
			if tc.Function.Name == "" {
				continue // custom (non-function) calls aren't runnable here
			}
			d.ToolCalls = append(d.ToolCalls, ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
	}
	out <- d
}

// assistantMessage replays an assistant turn, carrying any tool calls it
// made in the OpenAI wire shape (the API requires them before their
// RoleTool results).
func assistantMessage(m Message) openai.ChatCompletionMessageParamUnion {
	if len(m.ToolCalls) == 0 {
		return openai.AssistantMessage(m.Content)
	}
	asst := openai.ChatCompletionAssistantMessageParam{}
	if m.Content != "" {
		asst.Content.OfString = openai.String(m.Content)
	}
	for _, tc := range m.ToolCalls {
		asst.ToolCalls = append(asst.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: tc.ID,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			},
		})
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &asst}
}
