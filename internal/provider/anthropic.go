// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package provider

import (
	"context"
	"encoding/json"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
)

// anthropicMaxTokens is the Messages API's required output cap; used when
// the Prompt tab leaves max_tokens unset.
const anthropicMaxTokens = 4096

// anthropicSpec presents the Anthropic Messages API; the x-ratelimit
// fallbacks keep gateway-emitted limit policies visible.
//
// Documented headers: anthropic-ratelimit-{requests,tokens,input-tokens,
// output-tokens}-{limit,remaining,reset} and retry-after at
// https://docs.anthropic.com/en/api/rate-limits; request-id in the errors
// doc. No exhaustive official list of the rest exists.
// The messages wire hoists the system prompt to a top-level "system" and
// keeps everything else in "messages", where a tool call and its result
// are content blocks inside a turn rather than turns of their own.
var anthropicSpec = Spec{
	// Roles only — user and assistant — beside a top-level system and
	// tools, so assistant is the longest label this wire produces.
	Body: BodyShape{
		Items: "messages", Unit: "msg",
		Parts:      []string{"system", "tools"},
		LabelWidth: len("assistant"),
	},
	LogMeta: []MetricDef{
		// The generic provider id only; gateway-specific ids stay in
		// the Inspector's raw dump.
		Metric("request-id", "request-id"),
		Metric("tokens left", "anthropic-ratelimit-tokens-remaining", "x-ratelimit-remaining-tokens"),
		Metric("requests left", "anthropic-ratelimit-requests-remaining", "x-ratelimit-remaining-requests"),
	},
	RateLimits: []MetricDef{
		// The back-off on a 429: Anthropic sends retry-after in seconds; a
		// gateway limit policy in front may send retry-after-ms. Units live
		// in the label since the value passes through verbatim.
		{Candidates: []Candidate{
			{Header: "retry-after-ms", Label: "retry after (ms)"},
			{Header: "retry-after", Label: "retry after (s)"},
		}},
		Metric("requests left", "anthropic-ratelimit-requests-remaining", "x-ratelimit-remaining-requests"),
		Metric("requests max", "anthropic-ratelimit-requests-limit", "x-ratelimit-limit-requests"),
		Metric("tokens left", "anthropic-ratelimit-tokens-remaining", "x-ratelimit-remaining-tokens"),
		Metric("tokens max", "anthropic-ratelimit-tokens-limit", "x-ratelimit-limit-tokens"),
		Metric("consumed", "x-ratelimit-consumed-tokens"),
	},
	// The messages api's adjustable request properties:
	// https://docs.anthropic.com/en/api/messages
	Params: []ParamDef{
		{Name: "temperature", Hint: "0–1"},
		{Name: "top_p", Hint: "0–1"},
		{Name: "top_k", Hint: ""},
		{Name: "max_tokens", Hint: ""},
		{Name: "stop", Hint: "a,b,…"},
	},
}

// anthropicClient serves the anthropic provider via anthropic-sdk-go.
type anthropicClient struct {
	api   anthropic.Client
	model string
}

// newAnthropicClient serves the Anthropic Messages API on either host.
// Both use the same SDK and x-api-key auth; only the base URL differs:
//   - direct: https://api.anthropic.com          (SDK appends /v1/messages)
//   - azure:  https://<res>.services.ai.azure.com/anthropic  (→ /anthropic/v1/messages)
//
// Foundry accepts the native x-api-key and the default anthropic-version
// header, so the Azure host needs no route middleware — only the /anthropic
// base URL the profile supplies. (TODO: Entra ID Bearer auth on the Azure
// host, via an azidentity token wrapper — anthropic-sdk-go has no helper.)
func newAnthropicClient(cfg config.Config, captureCh chan<- capture.Event) *anthropicClient {
	opts := []option.RequestOption{
		option.WithHTTPClient(newHTTPClient(config.APIMessages, captureCh, cfg.Headers)),
		option.WithBaseURL(baseURL(cfg)), // {model} template filled; SDK appends /v1/messages
		// One HTTP request per send, as for the other providers.
		option.WithMaxRetries(0),
	}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey)) // x-api-key header
	}
	for k, v := range cfg.Headers {
		opts = append(opts, option.WithHeader(k, v))
	}
	for k, v := range cfg.Query {
		opts = append(opts, option.WithQueryAdd(k, v))
	}

	return &anthropicClient{api: anthropic.NewClient(opts...), model: cfg.Model}
}

// Stream starts a streaming message and feeds deltas into the returned
// channel until the response is complete.
func (c *anthropicClient) Stream(ctx context.Context, history []Message, p Params, overlay Overlay) <-chan Delta {
	out := make(chan Delta, 32)
	req := c.request(history, p)

	// The prompt's request overlay merges into the body verbatim, exactly as
	// in the openai client (raw fields ride WithJSONSet byte-for-byte).
	fields, err := overlay.Fields(config.APIMessages)
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
// never set.
func (c *anthropicClient) request(history []Message, p Params) anthropic.MessageNewParams {
	// The system prompt is a top-level field on the Messages API, not a
	// conversation turn.
	req := anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: anthropicMaxTokens,
	}
	for _, m := range history {
		switch m.Role {
		case RoleSystem:
			req.System = append(req.System, anthropic.TextBlockParam{Text: m.Content})
		case RoleAssistant:
			req.Messages = append(req.Messages, anthropic.NewAssistantMessage(assistantBlocks(m)...))
		case RoleTool:
			// A tool result is a user-message content block on this api.
			req.Messages = append(req.Messages, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false)))
		default:
			req.Messages = append(req.Messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		}
	}
	if p.Temperature != nil {
		req.Temperature = anthropic.Float(float64(*p.Temperature))
	}
	if p.TopP != nil {
		req.TopP = anthropic.Float(float64(*p.TopP))
	}
	if p.TopK > 0 {
		req.TopK = anthropic.Int(int64(p.TopK))
	}
	if p.MaxTokens > 0 {
		req.MaxTokens = int64(p.MaxTokens)
	}
	if len(p.Stop) > 0 {
		req.StopSequences = p.Stop
	}

	return req
}

// consume runs the one request and folds the event stream into deltas,
// closing out when the response is complete.
func (c *anthropicClient) consume(ctx context.Context, req anthropic.MessageNewParams, opts []option.RequestOption, out chan<- Delta) {
	defer close(out)

	stream := c.api.Messages.NewStreaming(ctx, req, opts...)
	defer stream.Close()

	// input tokens arrive on message_start, cumulative output tokens
	// on message_delta; both feed the final usage delta. The accumulator
	// folds the events into one Message so the Inspector can show the final
	// body instead of the raw event stream.
	var acc anthropic.Message
	var inTok, outTok int64
	for stream.Next() {
		event := stream.Current()
		_ = acc.Accumulate(event)
		switch ev := event.AsAny().(type) {
		case anthropic.MessageStartEvent:
			inTok = ev.Message.Usage.InputTokens
		case anthropic.ContentBlockDeltaEvent:
			if d, ok := ev.Delta.AsAny().(anthropic.TextDelta); ok && d.Text != "" {
				out <- Delta{Content: d.Text}
			}
		case anthropic.MessageDeltaEvent:
			outTok = ev.Usage.OutputTokens
		}
	}
	if err := stream.Err(); err != nil {
		out <- Delta{Err: err, Done: true}
		return
	}

	var usage *Usage
	if inTok > 0 || outTok > 0 {
		usage = &Usage{
			Prompt:     int(inTok),
			Completion: int(outTok),
			Total:      int(inTok + outTok),
		}
	}
	// Completed tool_use blocks come from the accumulated message; the
	// state layer decides whether to run them.
	d := Delta{Done: true, Usage: usage, Final: finalJSON(acc)}
	for _, block := range acc.Content {
		if tu, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
			d.ToolCalls = append(d.ToolCalls, ToolCall{
				ID:        tu.ID,
				Name:      tu.Name,
				Arguments: string(tu.Input),
			})
		}
	}
	out <- d
}

// assistantBlocks replays an assistant turn's content blocks, carrying any
// tool_use blocks it made (the API requires them before their tool_result).
func assistantBlocks(m Message) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion
	if m.Content != "" {
		blocks = append(blocks, anthropic.NewTextBlock(m.Content))
	}
	for _, tc := range m.ToolCalls {
		// The raw argument JSON marshals verbatim as the input object.
		blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, json.RawMessage(tc.Arguments), tc.Name))
	}
	if len(blocks) == 0 {
		blocks = append(blocks, anthropic.NewTextBlock(m.Content))
	}
	return blocks
}
