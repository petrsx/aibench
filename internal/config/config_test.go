// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

package config

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setAll pins every credential variable Resolve reads so values leaking
// from the developer's shell can't influence a test.
func setAll(t *testing.T, overrides map[string]string) {
	t.Helper()
	keys := []string{
		"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "AZURE_API_KEY",
		"AZURE_TENANT_ID", "AZURE_CLIENT_ID", "AZURE_CLIENT_SECRET",
		"AZURE_TOKEN", "AZURE_USE_LOGIN",
	}
	for _, k := range keys {
		t.Setenv(k, overrides[k])
	}
}

// TestTokenScopeDerivedFromRoute pins the audience rule: it follows the
// api the profile speaks, with no env or profile override. Azure's v1
// surface and the Foundry agent endpoints 401 on the classic audience.
func TestTokenScopeDerivedFromRoute(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"chat", Config{Kind: KindModel, API: APIChat}, scopeAzureAI},
		{"messages", Config{Kind: KindModel, API: APIMessages}, scopeAzureAI},
		{"responses", Config{Kind: KindModel, API: APIResponses}, scopeFoundry},
		{"agent", Config{Kind: KindAgent, API: APIResponses}, scopeFoundry},
	} {
		if got := tc.cfg.TokenScope(); got != tc.want {
			t.Errorf("%s: TokenScope() = %q; want %q", tc.name, got, tc.want)
		}
	}
}

func TestHasServicePrincipal(t *testing.T) {
	full := Config{TenantID: "t", ClientID: "c", ClientSecret: "s"}
	if !full.HasServicePrincipal() {
		t.Error("HasServicePrincipal() = false with all three set; want true")
	}
	partial := Config{TenantID: "t", ClientID: "c"}
	if partial.HasServicePrincipal() {
		t.Error("HasServicePrincipal() = true without secret; want false")
	}
}

// TestCredentialHelpIsActionable pins what the no-credentials error must
// carry: the file to edit, the names it already defines (never values),
// the unreferenced-key hint that explains how most people land here, and
// the fill-in menu.
func TestCredentialHelpIsActionable(t *testing.T) {
	got := CredentialHelp(".aibench/.env.dev", []string{"SUBSCRIPTION_KEY"}, false)
	for _, want := range []string{
		".aibench/.env.dev",    // which file to edit
		"SUBSCRIPTION_KEY",     // what it already defines
		"no auth method reads", // why that isn't enough
		"AZURE_TOKEN=",         // the menu
		"AZURE_USE_LOGIN=true",
		"AZURE_TENANT_ID",
		"api-key: ${SUBSCRIPTION_KEY}", // the headers escape hatch
		"docs/authentication.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("CredentialHelp() missing %q; got:\n%s", want, got)
		}
	}

	// A profile whose headers already use the key gets no "unreferenced"
	// hint and no headers suggestion — it landed here for another reason.
	used := CredentialHelp(".env", []string{"SUBSCRIPTION_KEY"}, true)
	if strings.Contains(used, "no auth method reads") || strings.Contains(used, "api-key: $") {
		t.Errorf("CredentialHelp(usedByHeaders=true) should not call the key unreferenced; got:\n%s", used)
	}

	// Env-only mode has no file and nothing defined: menu only, no
	// dangling "it defines" clause.
	bare := CredentialHelp("the environment", nil, false)
	if strings.Contains(bare, "it defines") {
		t.Errorf("CredentialHelp(nil defined) should not claim definitions; got:\n%s", bare)
	}
}

// TestResolveNoCredentialsNamesTheFile pins the plumbing: the sentinel
// reaches Resolve, which enriches it with the profile's own credentials
// file rather than a generic variable list.
func TestResolveNoCredentialsNamesTheFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "creds.env"), []byte("SUBSCRIPTION_KEY=s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	yaml := "profiles:\n  p:\n    kind: model\n    api:\n      provider: openai\n      base: https://x.example.net\n      model: m\n    auth:\n      credentials: creds.env\n"
	cfgPath := filepath.Join(dir, "aibench.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"OPENAI_API_KEY", "AZURE_TOKEN", "AZURE_USE_LOGIN", "AZURE_TENANT_ID", "AZURE_CLIENT_ID", "AZURE_CLIENT_SECRET"} {
		t.Setenv(k, "")
	}
	// Paths in the config resolve against the working directory, so the
	// test stands where its credentials file is.
	t.Chdir(dir)
	f, _, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Resolve("p")
	if err == nil {
		t.Fatal("Resolve() error = nil; want no-credentials help")
	}
	for _, want := range []string{`profile "p"`, "creds.env", "SUBSCRIPTION_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Resolve() error missing %q; got:\n%v", want, err)
		}
	}
	// The secret's value must never appear in an error.
	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("Resolve() error leaked a credential value:\n%v", err)
	}
}

// resolveWith resolves a one-profile config (credentials: env) with the
// given variables in the process environment — the way every credential
// rule is exercised now that the environment supplies secrets only.
func resolveWith(t *testing.T, provider string, env map[string]string) (Config, error) {
	t.Helper()
	setAll(t, env)
	path := filepath.Join(t.TempDir(), "aibench.yaml")
	yaml := "profiles:\n  p:\n    kind: model\n    api:\n      provider: " + provider +
		"\n      base: https://gw.example.net\n      model: m\n    auth:\n      credentials: env\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	f, _, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return f.Resolve("p")
}

// TestResolveAcceptsAnyCredential pins that each auth method stands on
// its own: one of them, and nothing else, is a complete configuration.
func TestResolveAcceptsAnyCredential(t *testing.T) {
	for name, tc := range map[string]struct {
		provider string
		creds    map[string]string
	}{
		"openai key":        {ProviderOpenAI, map[string]string{"OPENAI_API_KEY": "sk-x"}},
		"anthropic key":     {ProviderAnthropic, map[string]string{"ANTHROPIC_API_KEY": "sk-ant"}},
		"minted token":      {ProviderOpenAI, map[string]string{"AZURE_TOKEN": "eyJ0-token"}},
		"sign-in opt-in":    {ProviderOpenAI, map[string]string{"AZURE_USE_LOGIN": "true"}},
		"service principal": {ProviderOpenAI, map[string]string{"AZURE_TENANT_ID": "t", "AZURE_CLIENT_ID": "c", "AZURE_CLIENT_SECRET": "s"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := resolveWith(t, tc.provider, tc.creds); err != nil {
				t.Errorf("Resolve() with only the %s error = %v; want nil", name, err)
			}
		})
	}
}

// TestResolveRejectsCredentialConflicts pins the exactly-one rule: two
// credential methods is a named error, never a silent ladder pick. A
// partial service principal isn't a method at all — it can't
// authenticate anything, so it must not pass as one.
func TestResolveRejectsCredentialConflicts(t *testing.T) {
	sp := map[string]string{"AZURE_TENANT_ID": "t", "AZURE_CLIENT_ID": "c", "AZURE_CLIENT_SECRET": "s"}
	for name, tc := range map[string]struct {
		creds   map[string]string
		wantErr string
	}{
		"key and token":      {map[string]string{"OPENAI_API_KEY": "sk-x", "AZURE_TOKEN": "eyJ0"}, "credentials conflict"},
		"token and sign-in":  {map[string]string{"AZURE_TOKEN": "eyJ0", "AZURE_USE_LOGIN": "true"}, "credentials conflict"},
		"sign-in and sp":     {merge(sp, map[string]string{"AZURE_USE_LOGIN": "true"}), "credentials conflict"},
		"key and sp":         {merge(sp, map[string]string{"OPENAI_API_KEY": "sk-x"}), "credentials conflict"},
		"partial sp is none": {map[string]string{"AZURE_CLIENT_SECRET": "s"}, "no credentials"},
		"non-boolean opt-in": {map[string]string{"AZURE_USE_LOGIN": "yes please"}, "AZURE_USE_LOGIN"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := resolveWith(t, ProviderOpenAI, tc.creds)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Resolve() error = %v; want containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestResolveAnthropicKey pins the anthropic credential pick: the native
// ANTHROPIC_API_KEY first, else Foundry's AZURE_API_KEY.
func TestResolveAnthropicKey(t *testing.T) {
	for name, tc := range map[string]struct {
		env     map[string]string
		wantKey string
	}{
		"prefers ANTHROPIC_API_KEY":   {map[string]string{"ANTHROPIC_API_KEY": "sk-ant", "AZURE_API_KEY": "az"}, "sk-ant"},
		"falls back to AZURE_API_KEY": {map[string]string{"AZURE_API_KEY": "az-ant"}, "az-ant"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := resolveWith(t, ProviderAnthropic, tc.env)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if cfg.API != APIMessages || cfg.APIKey != tc.wantKey {
				t.Errorf("api, key = %q, %q; want messages, %q", cfg.API, cfg.APIKey, tc.wantKey)
			}
		})
	}
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	maps.Copy(out, a)
	maps.Copy(out, b)
	return out
}
