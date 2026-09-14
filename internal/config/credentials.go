// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package config

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The credentials contract: which secrets each auth method needs, how
// they assemble from the lookup chain, the exactly-one-method rule with
// its named errors, the help text that teaches the contract, and the
// token audience derived from the api. Secrets never live in the yaml —
// docs/authentication.md carries the user-facing model.

// anthropicKey picks the Anthropic credential: the native
// ANTHROPIC_API_KEY first, then the AZURE_API_KEY a Foundry deployment
// issues (both travel as x-api-key), falling back to whatever was
// already resolved.
func anthropicKey(env func(key, fallback string) string, current string) string {
	return env("ANTHROPIC_API_KEY", env("AZURE_API_KEY", current))
}

// buildCredentials reads only the secrets from the lookup chain, plus the
// non-endpoint defaults a profile doesn't carry. Endpoint config (the
// profile's api group — provider, type, store, base, model, headers,
// query — plus the prompt file and development) is the profile's job —
// the environment holds credentials, nothing else.
func buildCredentials(lookup func(string) string) (Config, error) {
	secret := func(key string) string {
		return strings.Trim(strings.TrimSpace(lookup(key)), `"`)
	}
	login, err := useLogin(secret("AZURE_USE_LOGIN"))
	if err != nil {
		return Config{}, err
	}
	return Config{
		APIKey:       secret("OPENAI_API_KEY"),
		TenantID:     secret("AZURE_TENANT_ID"),
		ClientID:     secret("AZURE_CLIENT_ID"),
		ClientSecret: secret("AZURE_CLIENT_SECRET"),
		Token:        secret("AZURE_TOKEN"),
		UseLogin:     login,
		// A default a profile may override; never sourced from env.
		PromptFile: "prompt.md",
	}, nil
}

// useLogin parses the AZURE_USE_LOGIN opt-in. Empty means off; anything
// that isn't a boolean is an error rather than a silently ignored knob.
func useLogin(v string) (bool, error) {
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("AZURE_USE_LOGIN %q: want true or false", v)
	}
	return b, nil
}

// validateCredentials pins the auth contract: **exactly one** credential
// method, plus whatever headers the profile declares. The methods each
// answer "who are you" a different way and would otherwise be resolved by
// a silent ladder — a credential that looks configured but never gets
// sent is precisely the confusion this tool exists to remove, so two of
// them is a named error. Headers are the exception by design: they
// compose, because a metering gateway wants its own key *alongside* the
// caller's backend credential.
func (c Config) validateCredentials() error {
	set := []string{}
	if c.APIKey != "" {
		set = append(set, keyVarFor(c.API))
	}
	if c.Token != "" {
		set = append(set, "AZURE_TOKEN")
	}
	if c.HasServicePrincipal() {
		set = append(set, "AZURE_TENANT_ID/AZURE_CLIENT_ID/AZURE_CLIENT_SECRET")
	}
	if c.UseLogin {
		set = append(set, "AZURE_USE_LOGIN")
	}
	switch {
	case len(set) > 1:
		return fmt.Errorf("%w: %s are all set — a profile authenticates one way. Keep the one this endpoint wants and delete the others (api.headers is separate and always sent on top)",
			ErrCredentialsConflict, strings.Join(set, " and "))
	case len(set) == 0 && len(c.Headers) == 0:
		return ErrNoCredentials
	}
	return nil
}

// ErrNoCredentials and ErrCredentialsConflict are the auth-contract
// failures. They travel as sentinels so the caller — which knows the
// credentials *source* and what it actually defines — can turn them into
// an error that names the file to edit. See CredentialHelp.
var (
	ErrNoCredentials       = errors.New("no credentials")
	ErrCredentialsConflict = errors.New("credentials conflict")
)

// credentialMethods is the fill-in menu both help texts print: one line
// per method, aligned, in the order a reader should consider them.
var credentialMethods = []struct{ set, does string }{
	{"OPENAI_API_KEY=…", "an API key the endpoint issued (openai wire)"},
	{"ANTHROPIC_API_KEY=…", "an API key the endpoint issued (anthropic wire)"},
	{"AZURE_TOKEN=…", "a token you minted: az account get-access-token"},
	{"AZURE_USE_LOGIN=true", "let aibench mint one from your az login"},
	{"AZURE_TENANT_ID / AZURE_CLIENT_ID / AZURE_CLIENT_SECRET", "a service principal"},
}

// CredentialHelp turns a bare ErrNoCredentials into something actionable:
// which file to edit, what that file already defines (names only — never
// values), and the menu of methods. `defined` is what the credentials
// source holds; unreferenced names are called out, because a file with a
// key in it that no profile uses is the most common way to land here.
func CredentialHelp(source string, defined []string, usedByHeaders bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "no credentials in %s", source)
	if len(defined) > 0 {
		sort.Strings(defined)
		fmt.Fprintf(&b, "\n\nit defines %s", strings.Join(defined, ", "))
		if !usedByHeaders {
			b.WriteString(", which no auth method reads and no api.headers entry references")
		}
	}
	// "in it" only reads correctly when the source is a file to open.
	if len(defined) > 0 {
		b.WriteString("\n\nset one of these in it:")
	} else {
		b.WriteString("\n\nset one of these:")
	}
	for _, m := range credentialMethods {
		fmt.Fprintf(&b, "\n  %-54s %s", m.set, m.does)
	}
	if len(defined) > 0 && !usedByHeaders {
		fmt.Fprintf(&b, "\n\nor send %s as a header from the profile:\n"+
			"  api:\n    headers:\n      api-key: ${%s}", defined[0], defined[0])
	}
	b.WriteString("\n\nsee docs/authentication.md")
	return b.String()
}

// keyVarFor names the api-key variable an api actually reads, so the
// conflict error names the variable the user wrote.
func keyVarFor(api string) string {
	if api == APIMessages {
		return "ANTHROPIC_API_KEY/AZURE_API_KEY"
	}
	return "OPENAI_API_KEY"
}

func (c Config) HasServicePrincipal() bool {
	return c.TenantID != "" && c.ClientID != "" && c.ClientSecret != ""
}

// Token audiences for Entra-issued bearer tokens. Which one a route
// wants is a property of the route, not a choice — so it is derived,
// never declared: Azure's v1 surface (the responses api) and the Foundry
// agent endpoints issue against ai.azure.com, everything else against
// the classic cognitiveservices audience. A wrong audience surfaces as a
// 401 with no hint, which is exactly why this isn't a user-facing knob.
const (
	scopeFoundry = "https://ai.azure.com/.default"
	scopeAzureAI = "https://cognitiveservices.azure.com/.default"
)

// TokenScope is the audience to request Entra tokens for on this
// endpoint. Derived from the api the profile speaks (see the scope
// constants); there is no override.
func (c Config) TokenScope() string {
	if c.API == APIResponses || c.Kind == KindAgent {
		return scopeFoundry
	}
	return scopeAzureAI
}
