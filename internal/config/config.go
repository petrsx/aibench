// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package config is aibench.yaml, the human's file: the profiles that say
// where requests go and how they authenticate, the prompt sets that say
// what is sent with them, and the strict parser + resolver that turn one
// profile into the effective Config a client is built from. The app
// never writes this file; what it keeps for itself is internal/prefs.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Provider is whose protocol the endpoint speaks (the SDK); API is which
// of that provider's wire shapes is used — the request/response format
// and presentation Spec. Selected by the profile's api.provider and
// api.type. Where the
// endpoint is served is entirely the base URL's business: api.base (a
// template that may carry {model}) plus query params describe any route —
// there is no host axis. Auth is likewise explicit: a plain API key goes
// in the provider's native form (Bearer / x-api-key), custom header
// schemes ride the profile's headers, and Entra credentials select token
// auth by their presence.
const (
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"

	APIChat      = "chat"      // OpenAI Chat Completions API (openai-go)
	APIResponses = "responses" // OpenAI Responses API (openai-go)
	APIMessages  = "messages"  // Anthropic Messages API (anthropic-sdk-go)

	// Kind is what stands behind the endpoint — a platform serves several
	// (Foundry: model deployments, agents, evaluations, MCP tools …) and
	// the client-side split follows from it: a model endpoint takes the
	// prompt from this tool, an agent owns its instructions and tools
	// server-side. Profiles must declare it (no default — readability);
	// further kinds (mcp, evaluations) arrive when the app can drive them.
	KindModel = "model" // model inference: this tool owns prompt + params
	KindAgent = "agent" // hosted agent: responses wire + server state implied, no prompt group
)

type Config struct {
	// AI endpoint
	Kind string // model or agent — what stands behind the endpoint (KindModel/KindAgent)
	API  string // chat, responses, or messages — the wire shape (SDK + Spec)
	// Store is the responses api's stateful mode (the official `store`
	// parameter): responses are stored server-side and each request chains
	// on the previous one via previous_response_id instead of replaying
	// the whole history. False = stateless replay. Responses api only —
	// validate rejects it elsewhere.
	Store bool
	// Stream is the official `stream` parameter: true streams deltas,
	// false makes one buffered request and the reply arrives whole.
	// Kind-aware default (model: true, agent: false — the hosted-agent
	// endpoint returns an empty event stream for stream:true); a
	// profile's api.stream declares an override. Non-streaming is
	// implemented on the responses api only — validate rejects it
	// elsewhere.
	Stream  bool
	APIBase string // base URL template; {model} fills with Model before the SDK appends the operation path
	APIKey  string // sent in the provider's native form: Bearer (openai) / x-api-key (anthropic)
	Model   string // model name (or the deployment name a {model} template fills)

	// Headers are extra request headers a profile declares (e.g. a
	// gateway's subscription key). The tool sends them verbatim and knows
	// nothing about any particular gateway; their values are redacted in
	// the capture.
	Headers map[string]string

	// Query are extra query parameters a profile declares (e.g. the
	// api-version a Foundry agent endpoint requires), sent verbatim on
	// every request. Unlike headers, values are NOT redacted in the
	// capture — keep secrets in headers.
	Query map[string]string

	// Entra ID service principal, used when no API key is provided.
	// The token's audience is not configurable — see TokenScope.
	TenantID     string
	ClientID     string
	ClientSecret string
	// Token (AZURE_TOKEN) is a bearer the user minted themselves —
	// `az account get-access-token`, or one they were handed. It rides
	// as Authorization: Bearer verbatim and the app acquires nothing.
	// Expires on its own; nothing here refreshes it.
	Token string
	// UseLogin (AZURE_USE_LOGIN) opts into azidentity's
	// DefaultAzureCredential chain, minting tokens from the user's own
	// az / azd sign-in. Deliberately not azidentity's own
	// AZURE_TOKEN_CREDENTIALS: that variable is a chain *selector*
	// ("dev", "prod", a credential name) which azidentity reads from the
	// process env and hard-errors on for any other value — a boolean
	// there would collide. Opting in is explicit; the app never reaches
	// for a sign-in the user didn't declare.
	UseLogin bool
	// PromptFile is the live-reloading prompt preset (markdown with YAML
	// frontmatter); the Prompt tab loads it and saves easy edits back.
	PromptFile string
	// StartersFiles are the active prompt set's starter scripts (each
	// file one scripted conversation; see preset.LoadStarters). Empty =
	// none; filled by File.ApplyPrompt.
	StartersFiles []string
	// ToolsFiles are the active prompt set's tool files (declarations +
	// bindings; see internal/bindings), merged in order. Filled by
	// File.ApplyPrompt from the top-level tools map.
	ToolsFiles []string
}

// normalize resolves the api from the provider/api knobs. The provider
// picks the api when none is given (openai → chat, anthropic → messages)
// and constrains it when one is (openai speaks chat and responses;
// anthropic only messages). Defaults: openai provider, chat api.
func normalize(provider, api string) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	api = strings.ToLower(strings.TrimSpace(api))
	switch provider {
	case "", ProviderOpenAI:
		switch api {
		case "":
			api = APIChat
		case APIChat, APIResponses:
		default:
			return "", fmt.Errorf("api %q not spoken by the openai provider (want chat or responses)", api)
		}
	case ProviderAnthropic:
		switch api {
		case "", APIMessages:
			api = APIMessages
		default:
			return "", fmt.Errorf("api %q not spoken by the anthropic provider (it speaks only messages; drop the api field)", api)
		}
	default:
		return "", fmt.Errorf("provider %q unknown (want openai or anthropic)", provider)
	}
	return api, nil
}

// ErrNoEndpoint is validation's nothing-configured signal: the profile
// gave no api.base.
var ErrNoEndpoint = errors.New("no endpoint: the profile has no api.base")

func (c Config) validate() error {
	switch c.Kind {
	case KindModel, KindAgent:
	default:
		return errors.New("kind must be model or agent")
	}
	switch c.API {
	case APIChat, APIResponses, APIMessages:
	default:
		return errors.New("api must be chat, responses, or messages")
	}
	if c.Store && c.API != APIResponses {
		return fmt.Errorf("store: true needs api type responses (%s keeps no server-side state)", c.API)
	}
	if !c.Stream && c.API != APIResponses {
		return fmt.Errorf("stream: false needs api type responses (the %s client has no buffered mode yet)", c.API)
	}
	if c.APIBase == "" {
		return ErrNoEndpoint
	}
	if err := c.validateCredentials(); err != nil {
		return err
	}
	return nil
}

func osLookup(key string) string { return os.Getenv(key) }
