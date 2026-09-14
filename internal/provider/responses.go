// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/azure"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/petrsx/aibench/internal/capture"
	"github.com/petrsx/aibench/internal/config"
)

// responsesSpec presents the OpenAI Responses API on either host. The
// rate-limit and request-id headers are the chat api's — same endpoints,
// same infrastructure — so those switches are shared; only the adjustable
// params differ (the Responses API takes no penalties, seed, or stop:
// https://platform.openai.com/docs/api-reference/responses/create).
// The responses wire hoists the system prompt out of the conversation
// ("instructions") and puts the structured-output schema under "text",
// while "input" carries messages and typed items side by side.
var responsesSpec = Spec{
	// function_call_output is the longest thing this api calls an input
	// item, and nothing among instructions/tools/text is close.
	Body: BodyShape{
		Items: "input", Unit: "item",
		Parts:      []string{"instructions", "tools", "text"},
		LabelWidth: len("function_call_output"),
	},
	LogMeta:    openaiSpec.LogMeta,
	RateLimits: openaiSpec.RateLimits,
	// Both sets are offered, because which ones apply is the model's
	// business, not the api's: a reasoning model rejects temperature and
	// top_p and takes reasoning.effort and text.verbosity, and an ordinary
	// one the other way round. An empty field is never sent, so the pair
	// that does not apply simply stays empty — and the endpoint's own
	// error is the honest answer when it does not.
	Params: []ParamDef{
		{Name: "temperature", Hint: "0–2"},
		{Name: "top_p", Hint: "0–1"},
		{Name: "max_output_tokens", Hint: ""},
		{Name: "reasoning.effort", Hint: "minimal–max"},
		{Name: "text.verbosity", Hint: "low/med/high"},
	},
}

// responsesClient serves the responses api via openai-go.
type responsesClient struct {
	api   openai.Client
	model string
	// store: the api's stateful mode (config `store: true`). Responses are
	// stored server-side and each request chains on the previous one via
	// previous_response_id, carrying only the new turns — instead of the
	// default stateless full-history replay.
	store bool
	// buffered: request without streaming and emit the complete response
	// as one delta — the declared stream: false (kind: agent's default:
	// the hosted-agent endpoint answers stream:true with 200
	// text/event-stream and an EMPTY body, verified live 2026-07-19,
	// while the plain request answers fully).
	buffered bool
}

// newResponsesClient builds the responses client. The route is entirely
// the base URL's business — a profile points at whatever surface serves
// /responses (Azure's v1 surface is {resource}/openai/v1, an APIM gateway
// its own prefix) and the SDK appends the operation path; baseURL fills a
// {model} template first. Auth follows the config's ladder: a key travels
// as Bearer, custom header schemes ride cfg.Headers verbatim, Entra
// credentials select token auth by their presence (pure header options,
// safe with any base URL). Entra note: Azure's v1 surface expects the
// https://ai.azure.com/.default audience, which Config.TokenScope derives
// from the api — never a declared knob.
func newResponsesClient(cfg config.Config, captureCh chan<- capture.Event) (*responsesClient, error) {
	opts := []option.RequestOption{
		option.WithHTTPClient(newHTTPClient(config.APIResponses, captureCh, cfg.Headers)),
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

	return &responsesClient{
		api:      openai.NewClient(opts...),
		model:    cfg.Model,
		store:    cfg.Store,
		buffered: !cfg.Stream,
	}, nil
}

// Stream starts a streaming response and feeds deltas into the returned
// channel until the response is complete.
func (c *responsesClient) Stream(ctx context.Context, history []Message, p Params, overlay Overlay) <-chan Delta {
	out := make(chan Delta, 32)
	req := c.request(history, p)

	// The prompt's request overlay merges into the body verbatim, exactly as
	// in the chat client (raw fields ride WithJSONSet byte-for-byte).
	fields, err := overlay.Fields(config.APIResponses)
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
// params the spec offers on it. Penalties, seed, and stop don't exist here;
// the spec never offers them, and foreign values are dropped by design.
func (c *responsesClient) request(history []Message, p Params) responses.ResponseNewParams {
	// In stateful mode, a chained request carries only the turns after
	// the last assistant reply (the server prepends the stored context);
	// instructions are NOT inherited across previous_response_id, so they
	// are re-sent from the full history either way.
	chained := c.store && p.PreviousResponseID != ""
	tail := history
	if chained {
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].Role == RoleAssistant {
				tail = history[i+1:]
				break
			}
		}
	}

	// The system prompt is the top-level instructions field on this api,
	// not an input item.
	var sys []string
	for _, m := range history {
		if m.Role == RoleSystem {
			sys = append(sys, m.Content)
		}
	}
	input := responses.ResponseInputParam{}
	for _, m := range tail {
		switch m.Role {
		case RoleSystem: // already collected above
		case RoleAssistant:
			// An assistant turn replays as a message item plus one
			// function_call item per call it made (the API requires them
			// before their function_call_output items).
			if m.Content != "" || len(m.ToolCalls) == 0 {
				input = append(input, responses.ResponseInputItemParamOfMessage(m.Content, responses.EasyInputMessageRoleAssistant))
			}
			for _, tc := range m.ToolCalls {
				input = append(input, responses.ResponseInputItemParamOfFunctionCall(tc.Arguments, tc.ID, tc.Name))
			}
		case RoleTool:
			input = append(input, responses.ResponseInputItemParamOfFunctionCallOutput(m.ToolCallID, m.Content))
		default:
			input = append(input, responses.ResponseInputItemParamOfMessage(m.Content, responses.EasyInputMessageRoleUser))
		}
	}

	req := responses.ResponseNewParams{
		Model: c.model,
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		// Stateless by default: the full history replays every turn, so the
		// Inspector always shows the complete request. `store: true`
		// flips to stored responses chained on previous_response_id.
		Store: openai.Bool(c.store),
	}
	if chained {
		req.PreviousResponseID = openai.String(p.PreviousResponseID)
	}
	if len(sys) > 0 {
		req.Instructions = openai.String(strings.Join(sys, "\n\n"))
	}
	if p.Temperature != nil {
		req.Temperature = openai.Float(float64(*p.Temperature))
	}
	if p.TopP != nil {
		req.TopP = openai.Float(float64(*p.TopP))
	}
	if p.MaxTokens > 0 {
		req.MaxOutputTokens = openai.Int(int64(p.MaxTokens))
	}
	if p.ReasoningEffort != "" {
		req.Reasoning.Effort = shared.ReasoningEffort(p.ReasoningEffort)
	}
	if p.Verbosity != "" {
		req.Text.Verbosity = responses.ResponseTextConfigVerbosity(p.Verbosity)
	}
	// Penalties, seed, and stop don't exist on this api; the spec never
	// offers them, and foreign values are dropped by design.

	return req
}

// consume runs the one request — buffered or streamed — and folds what comes
// back into deltas, closing out when the response is complete.
func (c *responsesClient) consume(ctx context.Context, req responses.ResponseNewParams, opts []option.RequestOption, out chan<- Delta) {
	defer close(out)

	// Buffered mode (hosted agents): one plain request, the whole
	// reply as a single content delta, then the same Done shape the
	// streamed path produces.
	if c.buffered {
		resp, err := c.api.Responses.New(ctx, req, opts...)
		if err != nil {
			out <- Delta{Err: err, Done: true}
			return
		}
		if text := resp.OutputText(); text != "" {
			out <- Delta{Content: text}
		}
		out <- c.doneDelta(resp)
		return
	}

	stream := c.api.Responses.NewStreaming(ctx, req, opts...)
	defer stream.Close()

	// No accumulator needed on this api: the response.completed event
	// carries the entire final Response — body, usage, and tool calls.
	var final *responses.Response
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta":
			if d := event.AsResponseOutputTextDelta().Delta; d != "" {
				out <- Delta{Content: d}
			}
		case "response.completed":
			r := event.AsResponseCompleted().Response
			final = &r
		}
	}
	if err := stream.Err(); err != nil {
		out <- Delta{Err: err, Done: true}
		return
	}
	out <- c.doneDelta(final)
}

// doneDelta builds the terminal delta from the completed response (nil
// when the stream ended without one): the raw final body for the
// Inspector, usage, the id a stored chain continues on, and any
// completed function calls — the state layer decides whether to run them.
func (c *responsesClient) doneDelta(final *responses.Response) Delta {
	d := Delta{Done: true}
	if final == nil {
		return d
	}
	if c.store {
		// The id the next request chains on; meaningless (and
		// unusable) without store, so stateless mode omits it.
		d.ResponseID = final.ID
	}
	// RawJSON is the wire body of the completed response; finalJSON
	// re-marshals as a fallback.
	if d.Final = final.RawJSON(); d.Final == "" {
		d.Final = finalJSON(*final)
	}
	if u := final.Usage; u.InputTokens > 0 || u.OutputTokens > 0 {
		d.Usage = &Usage{
			Prompt:     int(u.InputTokens),
			Completion: int(u.OutputTokens),
			Total:      int(u.TotalTokens),
		}
	}
	// CallID (not the item id) is what a function_call_output answers.
	for _, item := range final.Output {
		if item.Type == "function_call" {
			fc := item.AsFunctionCall()
			d.ToolCalls = append(d.ToolCalls, ToolCall{
				ID:        fc.CallID,
				Name:      fc.Name,
				Arguments: fc.Arguments,
			})
		}
	}
	return d
}
